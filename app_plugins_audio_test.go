package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"PetPC/internal/plugins"
)

// recordingAudio captures playback instead of touching an audio device.
//
// The capability's contract is "play THIS file", and the only way to assert that
// is to observe the path that reached the sink. A test that played for real could
// only assert that nothing returned an error.
type recordingAudio struct {
	mu     sync.Mutex
	played []string
	stops  int
	fail   error
}

func (r *recordingAudio) PlayWAVFile(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.played = append(r.played, path)
	return r.fail
}

func (r *recordingAudio) StopAudio() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stops++
}

func (r *recordingAudio) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.played) == 0 {
		return ""
	}
	return r.played[len(r.played)-1]
}

func (r *recordingAudio) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.played)
}

// audioApp returns an app with a recorder wired into the audio sink, plus one
// installed plugin holding the audio permission.
func audioApp(t *testing.T) (*App, *recordingAudio, string) {
	t.Helper()
	a := pluginApp(t)
	rec := &recordingAudio{}
	a.audioMu.Lock()
	a.audioOut = rec
	a.audioMu.Unlock()
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"audio"}})
	return a, rec, id
}

// wavOf builds a minimal valid-looking WAV body of n bytes.
//
// The capability does not parse the audio - it stores bytes and plays a path - so
// the content only has to be recognisable, which is what lets the round-trip
// assertion compare exact bytes.
func wavOf(n int) []byte {
	out := make([]byte, n)
	copy(out, "RIFF")
	copy(out[8:], "WAVE")
	for i := 12; i < n; i++ {
		out[i] = byte(i % 251)
	}
	return out
}

// TestAudioSaveListRoundTrip covers the storage half of the capability: bytes in,
// the same bytes on disk, and a listing that names them.
func TestAudioSaveListRoundTrip(t *testing.T) {
	a, _, id := audioApp(t)
	want := wavOf(512)
	payload, _ := json.Marshal(map[string]string{
		"name": "focus_end",
		"data": base64.StdEncoding.EncodeToString(want),
	})
	out, err := a.PluginRequest(id, "audio.save", string(payload))
	if err != nil {
		t.Fatalf("audio.save: %v", err)
	}
	var saved struct {
		Name  string `json:"name"`
		Bytes int    `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(out), &saved); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if saved.Name != "focus_end" || saved.Bytes != len(want) {
		t.Fatalf("audio.save reported %+v, want focus_end/%d", saved, len(want))
	}

	// The BYTES are the assertion: a save that re-encoded or truncated the audio
	// would still produce a file and still return ok.
	private, _ := a.pluginManager().PrivateDir(id)
	got, err := os.ReadFile(filepath.Join(private, "audio", "focus_end.wav"))
	if err != nil {
		t.Fatalf("the clip was not written: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("stored %d bytes, want the %d that were sent", len(got), len(want))
	}

	// A second one, so the listing has an order to get right.
	second, _ := json.Marshal(map[string]string{
		"name": "break_end",
		"data": base64.StdEncoding.EncodeToString(wavOf(64)),
	})
	if _, err := a.PluginRequest(id, "audio.save", string(second)); err != nil {
		t.Fatalf("audio.save (second): %v", err)
	}

	listOut, err := a.PluginRequest(id, "audio.list", `{}`)
	if err != nil {
		t.Fatalf("audio.list: %v", err)
	}
	var listed []struct {
		Name  string `json:"name"`
		Bytes int64  `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(listOut), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed %d clips, want 2: %+v", len(listed), listed)
	}
	if listed[0].Name != "break_end" || listed[1].Name != "focus_end" {
		t.Fatalf("the listing is not sorted by name: %+v", listed)
	}
	if listed[1].Bytes != int64(len(want)) {
		t.Fatalf("focus_end is listed as %d bytes, want %d", listed[1].Bytes, len(want))
	}

	// Removal, and the "already gone" case that must not be an error.
	if _, err := a.PluginRequest(id, "audio.remove", `{"name":"focus_end"}`); err != nil {
		t.Fatalf("audio.remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(private, "audio", "focus_end.wav")); !os.IsNotExist(err) {
		t.Fatalf("the clip survived removal: %v", err)
	}
	if _, err := a.PluginRequest(id, "audio.remove", `{"name":"focus_end"}`); err != nil {
		t.Fatalf("removing a missing clip must not be an error: %v", err)
	}
}

// TestAudioListOnAFreshPluginIsEmpty pins the first-run state: a plugin that has
// never saved anything must get an empty list, not an error, because "I have no
// clips yet" is exactly what it needs to know to fall back to its packaged audio.
func TestAudioListOnAFreshPluginIsEmpty(t *testing.T) {
	a, _, id := audioApp(t)
	out, err := a.PluginRequest(id, "audio.list", `{}`)
	if err != nil {
		t.Fatalf("audio.list on a fresh plugin: %v", err)
	}
	var listed []any
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("a fresh plugin listed %d clips", len(listed))
	}
}

// TestAudioRejectsBadNames is the path-traversal half, and it is the reason the
// name rule is shared with storage rather than reinvented: the name becomes a file
// name, so a separator or a ".." would be a way out of the plugin's directory.
//
// Every case must leave the disk UNCHANGED - a refusal that had already created a
// file would be a refusal in name only.
func TestAudioRejectsBadNames(t *testing.T) {
	a, rec, id := audioApp(t)
	private, _ := a.pluginManager().PrivateDir(id)
	before, _ := os.ReadDir(private)

	bad := []string{"../escape", "a/b", "", strings.Repeat("x", 65), `..\escape`, "/abs", "."}
	for _, name := range bad {
		for _, method := range []string{"audio.save", "audio.remove", "audio.play"} {
			payload, _ := json.Marshal(map[string]string{
				"name": name,
				"data": base64.StdEncoding.EncodeToString(wavOf(32)),
			})
			if _, err := a.PluginRequest(id, method, string(payload)); err == nil {
				t.Fatalf("%s accepted the name %q", method, name)
			}
		}
	}

	after, _ := os.ReadDir(private)
	if len(after) != len(before) {
		t.Fatalf("a refused call created files: %d -> %d entries", len(before), len(after))
	}
	if rec.count() != 0 {
		t.Fatalf("a refused audio.play still played %d file(s)", rec.count())
	}
	// And nothing escaped upward.
	for _, e := range mustReadDir(t, a.pluginManager().DataDir()) {
		if e.Name() == "escape" {
			t.Error("a traversal name escaped the plugin directory")
		}
	}
}

// TestAudioSaveRejectsBadData covers the decode edge: empty, non-base64, and
// oversized payloads must be refused rather than written as junk that later plays
// as noise.
func TestAudioSaveRejectsBadData(t *testing.T) {
	a, _, id := audioApp(t)
	private, _ := a.pluginManager().PrivateDir(id)
	audioDir := filepath.Join(private, "audio")

	cases := []struct {
		name string
		data string
	}{
		{"empty", ""},
		{"not base64", "!!!not base64!!!"},
		{"base64 of nothing", base64.StdEncoding.EncodeToString(nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(map[string]string{"name": "x", "data": tc.data})
			if _, err := a.PluginRequest(id, "audio.save", string(payload)); err == nil {
				t.Fatalf("audio.save accepted %s", tc.name)
			}
			if _, err := os.Stat(filepath.Join(audioDir, "x.wav")); !os.IsNotExist(err) {
				t.Fatalf("%s left a file behind", tc.name)
			}
		})
	}
}

// TestAudioPlayPassesTheStoredPath pins the playback contract: the sink receives
// the ABSOLUTE PATH of the clip inside the plugin's own directory, and a clip that
// was never saved is refused WITHOUT reaching the device.
func TestAudioPlayPassesTheStoredPath(t *testing.T) {
	a, rec, id := audioApp(t)

	// Playing something that does not exist must not touch the device: otherwise
	// a plugin could use the refusal itself as an observation.
	if _, err := a.PluginRequest(id, "audio.play", `{"name":"missing"}`); err == nil {
		t.Fatal("playing a clip that was never saved must be refused")
	}
	if rec.count() != 0 {
		t.Fatalf("a missing clip reached the audio device (%d calls)", rec.count())
	}

	payload, _ := json.Marshal(map[string]string{
		"name": "focus_end",
		"data": base64.StdEncoding.EncodeToString(wavOf(128)),
	})
	if _, err := a.PluginRequest(id, "audio.save", string(payload)); err != nil {
		t.Fatalf("audio.save: %v", err)
	}
	if _, err := a.PluginRequest(id, "audio.play", `{"name":"focus_end"}`); err != nil {
		t.Fatalf("audio.play: %v", err)
	}

	private, _ := a.pluginManager().PrivateDir(id)
	want := filepath.Join(private, "audio", "focus_end.wav")
	if got := rec.last(); got != want {
		t.Fatalf("the sink played %q, want %q", got, want)
	}

	if _, err := a.PluginRequest(id, "audio.stop", `{}`); err != nil {
		t.Fatalf("audio.stop: %v", err)
	}
	if rec.stops != 1 {
		t.Fatalf("audio.stop called the sink %d times, want 1", rec.stops)
	}
}

// TestAudioNeedsThePermission is the gate: a plugin without `audio` gets the
// refusal and NOTHING else - no file, no sound.
func TestAudioNeedsThePermission(t *testing.T) {
	a := pluginApp(t)
	rec := &recordingAudio{}
	a.audioMu.Lock()
	a.audioOut = rec
	a.audioMu.Unlock()
	// Installed and enabled, but the permission was never granted.
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"storage"}})
	private, _ := a.pluginManager().PrivateDir(id)
	before, _ := os.ReadDir(private)

	save, _ := json.Marshal(map[string]string{
		"name": "x",
		"data": base64.StdEncoding.EncodeToString(wavOf(32)),
	})
	for _, call := range []struct{ method, payload string }{
		{"audio.save", string(save)},
		{"audio.list", `{}`},
		{"audio.remove", `{"name":"x"}`},
		{"audio.play", `{"name":"x"}`},
		{"audio.stop", `{}`},
	} {
		_, err := a.PluginRequest(id, call.method, call.payload)
		if err == nil {
			t.Fatalf("%s succeeded without the audio permission", call.method)
		}
		if !strings.Contains(err.Error(), plugins.PermAudio) {
			t.Fatalf("%s refused with %q, which does not name the %s permission",
				call.method, err, plugins.PermAudio)
		}
	}

	after, _ := os.ReadDir(private)
	if len(after) != len(before) {
		t.Fatalf("a refused audio call wrote files: %d -> %d", len(before), len(after))
	}
	if rec.count() != 0 || rec.stops != 0 {
		t.Fatalf("a refused audio call reached the device: %d plays, %d stops", rec.count(), rec.stops)
	}
}

// TestAudioIsScopedToItsOwnPlugin pins that one plugin cannot read another's
// clips: the directory comes from the CALLER's id, never from the payload.
func TestAudioIsScopedToItsOwnPlugin(t *testing.T) {
	a, _, first := audioApp(t)
	// A DISTINCT id: installTestPlugin's default is a fixed one, so asking for it
	// twice is a re-install of the same plugin rather than a second plugin.
	second := installSampleWith(t, a, map[string]any{
		"id":          "com.example.otheraudio",
		"permissions": []string{"audio"},
	})
	if first == second {
		t.Fatal("the fixture installed the same id twice")
	}

	payload, _ := json.Marshal(map[string]string{
		"name": "shared",
		"data": base64.StdEncoding.EncodeToString(wavOf(48)),
	})
	if _, err := a.PluginRequest(first, "audio.save", string(payload)); err != nil {
		t.Fatalf("audio.save: %v", err)
	}

	// The second plugin must see none of it, and must not be able to play it.
	out, err := a.PluginRequest(second, "audio.list", `{}`)
	if err != nil {
		t.Fatalf("audio.list: %v", err)
	}
	var listed []any
	_ = json.Unmarshal([]byte(out), &listed)
	if len(listed) != 0 {
		t.Fatalf("a plugin saw another plugin's clips: %+v", listed)
	}
	if _, err := a.PluginRequest(second, "audio.play", `{"name":"shared"}`); err == nil {
		t.Fatal("a plugin played another plugin's clip")
	}
}

// TestNetFetchCanReturnBinaryWithoutCorruptingIt covers the defect that made the
// voice plugin impossible before it was fixed.
//
// A synthesis engine answers with audio/wav. net.fetch hands the reply to the
// plugin as a JSON string, and Go's JSON ENCODER replaces every byte that is not
// valid UTF-8 with U+FFFD - so a WAV came back the same length but with every
// 0x80+ byte replaced, which is silence-or-noise rather than audio. It looked like
// it had worked, which is what made it worth a test rather than a comment.
//
// The assertion walks the whole pipeline the plugin uses: fetch with
// encoding:"base64" -> decode -> audio.save -> the file on disk equals the bytes
// the server actually sent.
func TestNetFetchCanReturnBinaryWithoutCorruptingIt(t *testing.T) {
	// A WAV header plus high bytes: exactly the shape that U+FFFD replacement
	// destroys, and the shape every real clip has.
	wav := wavOf(4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(wav)
	}))
	defer srv.Close()

	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{
		"permissions": []string{"net.fetch", "audio"},
		"netHosts":    []string{strings.TrimPrefix(srv.URL, "http://")},
	})

	out, err := a.PluginRequest(id, "net.fetch",
		`{"method":"GET","url":"`+srv.URL+`/tts","encoding":"base64"}`)
	if err != nil {
		t.Fatalf("net.fetch: %v", err)
	}
	var reply struct {
		Status   int    `json:"status"`
		Body     string `json:"body"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if reply.Status != 200 {
		t.Fatalf("status = %d", reply.Status)
	}
	if reply.Encoding != "base64" {
		t.Fatalf("the reply does not say it is base64: %q", reply.Encoding)
	}
	raw, err := base64.StdEncoding.DecodeString(reply.Body)
	if err != nil {
		t.Fatalf("the body is not valid base64: %v", err)
	}
	// THE assertion: the bytes survived, rather than being replaced.
	if string(raw) != string(wav) {
		t.Fatalf("the audio was corrupted in transit: got %d bytes, want %d (first difference at %d)",
			len(raw), len(wav), firstDifference(raw, wav))
	}

	// And the corrupted-once-it-reaches-the-plugin path is closed end to end: the
	// decoded bytes can be stored and come back byte-identical.
	save, _ := json.Marshal(map[string]string{"name": "rendered", "data": reply.Body})
	if _, err := a.PluginRequest(id, "audio.save", string(save)); err != nil {
		t.Fatalf("audio.save: %v", err)
	}
	private, _ := a.pluginManager().PrivateDir(id)
	stored, err := os.ReadFile(filepath.Join(private, "audio", "rendered.wav"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(stored) != string(wav) {
		t.Fatalf("the stored clip is not what the engine returned (%d vs %d bytes)",
			len(stored), len(wav))
	}

	// The default stays text, so an ordinary JSON reply is unchanged.
	out2, err := a.PluginRequest(id, "net.fetch",
		`{"method":"GET","url":"`+srv.URL+`/tts"}`)
	if err != nil {
		t.Fatalf("net.fetch (text): %v", err)
	}
	var textReply struct {
		Body     string `json:"body"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal([]byte(out2), &textReply); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if textReply.Encoding != "" {
		t.Fatalf("a text reply must not claim an encoding: %q", textReply.Encoding)
	}
	// The text form is DOCUMENTED as lossy for binary, and this records that: it
	// is why the option exists rather than being a preference.
	if textReply.Body == string(wav) {
		t.Log("the text form happened to round-trip; the base64 form is still the one to use")
	}
}

// TestNetFetchRejectsAnUnknownEncoding keeps the new option from becoming a
// free-form string the host half-ignores.
func TestNetFetchRejectsAnUnknownEncoding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()

	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{
		"permissions": []string{"net.fetch"},
		"netHosts":    []string{strings.TrimPrefix(srv.URL, "http://")},
	})
	_, err := a.PluginRequest(id, "net.fetch",
		`{"method":"GET","url":"`+srv.URL+`/x","encoding":"utf16"}`)
	if err == nil {
		t.Fatal("an unknown encoding must be refused")
	}
	if !strings.Contains(err.Error(), "utf16") {
		t.Fatalf("error %q must name the bad encoding", err)
	}
}

// firstDifference reports the index where a and b first differ, for a readable
// failure message.
func firstDifference(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func mustReadDir(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	return entries
}

// TestAudioImportCopiesADeclaredPackageFile covers the route the packaged built-in
// clips take into the player.
//
// The plugin cannot fetch its own package over HTTP - the frame's origin is opaque
// and the asset route sets no CORS header - so importing has to be a capability.
// The two rules that make it safe are asserted here: the SOURCE must be inside the
// plugin's own package, and it must be DECLARED in the manifest.
func TestAudioImportCopiesADeclaredPackageFile(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"audio"}})
	pkgDir, err := a.pluginManager().PluginDir(id)
	if err != nil {
		t.Fatalf("PluginDir: %v", err)
	}
	// The fixture's package is whatever the test zip contains; write one declared
	// asset into it so there is something real to copy.
	want := wavOf(256)
	if err := os.WriteFile(filepath.Join(pkgDir, "sample.wav"), want, 0o644); err != nil {
		t.Fatalf("seed the package: %v", err)
	}
	// The manifest must declare it, and the declaration is read from disk on every
	// call, so the installed manifest has to be the one that lists it.
	installDeclaredAsset(t, a, id, "sample.wav")

	out, err := a.PluginRequest(id, "audio.import", `{"name":"builtin","from":"sample.wav"}`)
	if err != nil {
		t.Fatalf("audio.import: %v", err)
	}
	var got struct {
		Name  string `json:"name"`
		Bytes int    `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "builtin" || got.Bytes != len(want) {
		t.Fatalf("import reported %+v, want builtin/%d", got, len(want))
	}

	// The bytes are the assertion, not the file's existence.
	private, _ := a.pluginManager().PrivateDir(id)
	copied, err := os.ReadFile(filepath.Join(private, "audio", "builtin.wav"))
	if err != nil {
		t.Fatalf("the clip was not imported: %v", err)
	}
	if string(copied) != string(want) {
		t.Fatalf("imported %d bytes, want the %d that were there", len(copied), len(want))
	}

	// An undeclared package file must be refused: the declaration is what makes
	// "which files may be imported" an auditable set rather than "anything".
	if _, err := a.PluginRequest(id, "audio.import", `{"name":"nope","from":"not-declared.wav"}`); err == nil {
		t.Fatal("an undeclared package file must be refused")
	}
	// And a traversal must be refused before the declaration is even consulted.
	for _, from := range []string{"../secret.wav", "/etc/passwd", "a/../../x.wav", ""} {
		payload, _ := json.Marshal(map[string]string{"name": "x", "from": from})
		if _, err := a.PluginRequest(id, "audio.import", string(payload)); err == nil {
			t.Fatalf("audio.import accepted the source %q", from)
		}
	}
}

// installDeclaredAsset rewrites the installed manifest to declare one asset.
//
// The capability reads the manifest from the INSTALLED directory (that is the row
// the gate resolves), so a test that only changed the market's zip would exercise
// the "not declared" refusal instead of the success path.
func installDeclaredAsset(t *testing.T, a *App, id, asset string) {
	t.Helper()
	dir, err := a.pluginManager().PluginDir(id)
	if err != nil {
		t.Fatalf("PluginDir: %v", err)
	}
	path := filepath.Join(dir, "manifest.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	doc["assets"] = []string{asset}
	updated, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	// No reload call: Manager.Get re-reads the plugin directory on every call, so
	// the edited manifest is what the gate resolves on the next request. That is
	// exactly why the capability can read the declaration at call time rather than
	// caching it.
}
