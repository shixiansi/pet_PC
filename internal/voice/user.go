package voice

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// This file is the USER-VOICE half of the package.
//
// voice.go owns the lines the binary ships with; this owns the lines the user
// renders for themselves. The two are deliberately separate stores, because they
// have opposite lifetimes: the embedded clips are rebuilt with the executable and
// thrown away on every upgrade, while a role's renders, the take the user picked
// and the parameters that produced it must survive upgrades - they are minutes of
// CPU time and a judgement call that cannot be recovered from the source tree.
//
// Hence: embedded clips live on disk only as a transient materialisation under
// voice/, while user renders live under voice/user/ and are never cleaned up by
// Cleanup (which only ever deletes the petpc-* files it wrote itself).
//
// The unit of work is a TAKE, not a line. That is the whole answer to "the
// generated line has a defect": re-rendering appends another take instead of
// overwriting, so the previous one stays available for comparison, and only the
// chosen take is ever played. A line's audio is therefore an append-only list plus
// a pointer, which is what makes "regenerate just this sentence until it is right"
// safe - no step destroys audio the user might still prefer.

// Role is one character the pet can speak as.
//
// It carries the three paths GPT-SoVITS needs (its two weight files and the
// reference clip that fixes the timbre) plus the sampling knobs that decide how
// the voice reads. The knobs are per-role rather than global because they are not
// interchangeable: a reference clip recorded faster or flatter needs different
// speed and expression settings to sound natural, so "which sounds right" is a
// property of the pair, not of the character.
type Role struct {
	// ID is a slug, and is also the directory a role's models conventionally live
	// in. It is the stable key: Name is for display and may be edited.
	ID string `json:"id"`
	// Name is what the settings panel shows, e.g. 可莉.
	Name string `json:"name"`
	// GPT and SoVITS are the two weight files.
	GPT string `json:"gpt"`
	// SoVITS is the VITS (tone) weight file.
	SoVITS string `json:"sovits"`
	// Ref is the reference audio whose timbre is cloned.
	Ref string `json:"ref"`
	// PromptText is the transcript of Ref.
	//
	// It is optional here (the engine accepts an empty one and infers), and it is
	// worth leaving empty unless the reference is clean: a transcript that does not
	// match the audio degrades the result more than a missing one does.
	PromptText string `json:"promptText"`
	// PromptLang is the language of Ref and PromptText.
	PromptLang string `json:"promptLang"`
	// Lang is the language to synthesise the pet's lines in.
	Lang string `json:"lang"`

	// --- sampling. These decide naturalness and stability, not the timbre. ---

	// Speed scales the reading rate (the engine's speed_factor: >1 is faster).
	Speed float64 `json:"speed"`
	// Temperature is how much variation the sampler allows. Lower is flatter but
	// more repeatable; this is the knob that trades liveliness against stability.
	Temperature float64 `json:"temperature"`
	// TopK and TopP cut off the sampling distribution's tail. Lower means fewer
	// bizarre readings and less variety.
	TopK int     `json:"topK"`
	TopP float64 `json:"topP"`
	// RepetitionPenalty discourages the stutter that long Chinese sentences
	// sometimes produce, where the model repeats a syllable until the clip ends.
	RepetitionPenalty float64 `json:"repetitionPenalty"`
	// Seed makes a take reproducible. -1 means "let the engine choose", which is
	// what you want while auditioning and NOT what you want once a line is right.
	Seed int `json:"seed"`
}

// Normalise fills in defaults for anything left at its zero value.
//
// A role assembled by hand - the panel's "add role" form, or a config file someone
// edited - will be missing most of these, and every one of them has a sane default.
// Doing it here means no other code has to ask whether TopK is 0 because the user
// meant it or because nobody set it.
func (r *Role) Normalise() {
	r.ID = slug(r.ID)
	if r.ID == "" {
		r.ID = slug(r.Name)
	}
	if r.Name == "" {
		r.Name = r.ID
	}
	if r.Lang == "" {
		r.Lang = "zh"
	}
	if r.PromptLang == "" {
		r.PromptLang = r.Lang
	}
	if r.Speed <= 0 {
		r.Speed = 1
	}
	if r.Temperature <= 0 {
		r.Temperature = 1
	}
	if r.TopK <= 0 {
		r.TopK = 15
	}
	if r.TopP <= 0 {
		r.TopP = 1
	}
	if r.RepetitionPenalty <= 0 {
		r.RepetitionPenalty = 1.35
	}
	if r.Seed == 0 {
		r.Seed = -1
	}
}

// Validate reports whether a role can actually be sent to the engine.
//
// Checked before a render rather than at save time: a role with a missing model is
// a perfectly reasonable thing to have half-typed into the form, and the failure
// that matters is the one where a render is attempted.
func (r Role) Validate() error {
	switch {
	case r.ID == "":
		return errors.New("角色缺少 ID")
	case strings.TrimSpace(r.Name) == "":
		return errors.New("角色缺少名称")
	case strings.TrimSpace(r.GPT) == "":
		return errors.New("角色缺少 GPT 权重路径")
	case strings.TrimSpace(r.SoVITS) == "":
		return errors.New("角色缺少 SoVITS 权重路径")
	case strings.TrimSpace(r.Ref) == "":
		return errors.New("角色缺少参考音频路径")
	}
	return nil
}

// Take is one rendered attempt at a line.
//
// Takes are append-only and never renumbered, because the chosen take is recorded
// by version: renumbering would silently repoint every line whose version sat above
// a deletion.
type Take struct {
	// Version is 1-based and unique within its line.
	Version int `json:"version"`
	// Role is the role ID whose voice this was rendered with, so the studio can say
	// why two takes sound different.
	Role string `json:"role"`
	// RoleName is that role's display name at render time. Stored rather than
	// looked up so a take stays self-describing after the role is renamed/removed.
	RoleName string `json:"roleName"`
	// Text is the sentence as rendered. Stored because the line's wording can be
	// changed later, and a take must stay explainable by the text that produced it.
	Text string `json:"text"`
	// File is the clip's name inside the line's directory ("v003.wav").
	File string `json:"file"`
	// Bytes and DurationMS describe the clip, so the UI can show its size and
	// length without reading it.
	Bytes      int64 `json:"bytes"`
	DurationMS int   `json:"durationMs"`
	// Params is the role's sampling configuration at render time. Kept so a good
	// take can be reproduced and a bad one explained.
	Params Role `json:"params"`
	// GeneratedAt is when it was rendered.
	GeneratedAt time.Time `json:"generatedAt"`
}

// LineSlot is a line's takes and which one is in use.
type LineSlot struct {
	// Takes is every take ever rendered for this line, oldest first.
	Takes []Take `json:"takes"`
	// Chosen is the version currently played, or 0 for "none yet", in which case
	// the embedded clip is used instead.
	Chosen int `json:"chosen"`
}

// Settings is the whole persisted user-voice configuration.
type Settings struct {
	// ActiveRole is the role a render uses unless another is named explicitly.
	ActiveRole string `json:"activeRole"`
	// Roles is every character the user has set up.
	Roles []Role `json:"roles"`
	// Lines maps a line to its takes and choice. A line absent from the map has
	// never been rendered and plays its embedded clip.
	Lines map[Line]LineSlot `json:"lines"`
}

// SubdirName is the directory user renders live in, under the data directory's
// voice folder. Exported so the app can show the user where their audio went.
const SubdirName = "user"

// indexName is the settings file. It is JSON, like every other file the app owns,
// so a user who wants to hand-edit a role's parameters can.
const indexName = "voice-user.json"

// clipsDirName holds the rendered audio, one directory per line.
const clipsDirName = "clips"

// Store is the on-disk user-voice state.
//
// It is safe for concurrent use. The lock is held across the small index writes but
// never across a network render, which is the only operation here that takes
// seconds.
type Store struct {
	dir string

	mu  sync.Mutex
	set Settings
	// paths caches the clip resolved for a line, TAGGED WITH THE ROLE it was
	// resolved for, so playback does not stat a file on every timer tick.
	//
	// The tag is what makes the cache safe now that resolution depends on the
	// current role: an entry resolved for 可莉 must never be handed back after the
	// user switched to 纳西妲. Without it the cache would reintroduce exactly the
	// stale-voice bug this resolution path exists to remove.
	paths map[Line]cachedClip

	// loadedAt and loadedSize are the index file's modification time and size when
	// it was last read, and loadedMod is whether that read happened at all.
	//
	// They exist because the index is now written by MORE THAN ONE PROCESS: the
	// standalone voice studio edits it while this app is running, and the pet must
	// notice. Without this check the pet kept playing the take it had cached at
	// startup - so choosing a different version in the studio appeared to do
	// nothing until the pet was restarted, which is precisely the "I changed it and
	// nothing happened" failure the studio is meant to eliminate.
	loadedMod  bool
	loadedAt   time.Time
	loadedSize int64
}

// cachedClip is one entry of the playback path cache, tagged with its role.
//
// role is the id the clip was resolved for (BuiltInRoleID for the embedded clip).
// A mismatch means the entry is unusable, which is how a role switch invalidates
// the cache without the store having to be told about it.
type cachedClip struct {
	role string
	path string
}

// Open loads (or creates) the user-voice store rooted at base/user.
//
// base is the same directory the embedded clips are materialised into, so all of
// the app's voice data sits under one path the user can find and delete.
func Open(base string) (*Store, error) {
	dir := filepath.Join(base, SubdirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建语音目录失败：%w", err)
	}
	s := &Store{dir: dir, set: Settings{Roles: nil, Lines: map[Line]LineSlot{}}, paths: map[Line]cachedClip{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// Dir returns the directory backing the store.
func (s *Store) Dir() string { return s.dir }

func (s *Store) indexPath() string { return filepath.Join(s.dir, indexName) }

// reloadIfChanged re-reads the index when another process has rewritten it.
//
// The caller must NOT hold s.mu. It is called from every read path that decides
// what to play, because a stale answer there is a wrong sound rather than a stale
// number on screen.
//
// A stat failure is ignored rather than treated as "changed": a momentarily
// unreadable file is not evidence of an edit, and reloading into a failed read
// would discard good state.
func (s *Store) reloadIfChanged() {
	fi, err := os.Stat(s.indexPath())
	if err != nil {
		return
	}
	s.mu.Lock()
	unchanged := s.loadedMod && fi.ModTime().Equal(s.loadedAt) && fi.Size() == s.loadedSize
	s.mu.Unlock()
	if unchanged {
		return
	}

	set, ok := readSettings(s.indexPath())
	if !ok {
		// The file is mid-write or unreadable. Keep what we have: the next call
		// will try again, and the writer is atomic (temp + rename) so this window
		// is tiny.
		return
	}

	s.mu.Lock()
	s.set = set
	// Every cached path is now suspect: the chosen take may have moved.
	s.paths = map[Line]cachedClip{}
	s.loadedMod = true
	s.loadedAt = fi.ModTime()
	s.loadedSize = fi.Size()
	s.mu.Unlock()
}

// readSettings parses an index file. ok is false when it is missing, empty or
// malformed - all of which mean "keep the current state" rather than "reset".
func readSettings(path string) (Settings, bool) {
	set := Settings{Lines: map[Line]LineSlot{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return set, false
	}
	data = trimBOM(data)
	if len(strings.TrimSpace(string(data))) == 0 {
		return set, false
	}
	if err := json.Unmarshal(data, &set); err != nil {
		return set, false
	}
	if set.Lines == nil {
		set.Lines = map[Line]LineSlot{}
	}
	for i := range set.Roles {
		set.Roles[i].Normalise()
	}
	return set, true
}

// load reads the index, tolerating a missing or malformed file.
//
// A malformed index is NOT fatal and is not quarantined either: it is a settings
// file whose only unrecoverable content is which take was chosen, and refusing to
// start the pet over it would be absurd. It is reset, and the rendered clips stay
// on disk where the user can still play them.
func (s *Store) load() error {
	set, ok := readSettings(s.indexPath())
	if !ok {
		// Missing or unreadable: keep the defaults. Note the file is NOT marked as
		// loaded, so the next reloadIfChanged will pick it up as soon as it exists -
		// which matters on first run, where the studio creates it moments later.
		if _, err := os.Stat(s.indexPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	s.set = set
	// load() holds no lock here, so this uses the locking form.
	s.markLoaded()
	return nil
}

// markLoaded records the index file's current stamp.
//
// The caller must NOT hold s.mu; see markLoadedLocked for the other case.
func (s *Store) markLoaded() {
	fi, err := os.Stat(s.indexPath())
	if err != nil {
		return
	}
	s.mu.Lock()
	s.loadedMod = true
	s.loadedAt = fi.ModTime()
	s.loadedSize = fi.Size()
	s.mu.Unlock()
}

// trimBOM strips a UTF-8 byte order mark, which Notepad writes and encoding/json
// rejects. Users are expected to edit this file by hand.
func trimBOM(b []byte) []byte {
	return []byte(strings.TrimPrefix(string(b), "\ufeff"))
}

// save writes the index atomically.
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.set, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(s.dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, s.indexPath()); err != nil {
		return err
	}
	// Record our own write, so the next reloadIfChanged does not treat our own save
	// as somebody else's edit and re-read it needlessly.
	//
	// The UNLOCKED variant, because every caller of save() already holds s.mu:
	// calling the locking markLoaded() here deadlocked every write in this package.
	s.markLoadedLocked()
	return nil
}

// markLoadedLocked is markLoaded for callers that already hold s.mu.
//
// It re-stats the file rather than taking the size and time from the write, because
// the value that matters is what a LATER stat will see - and on Windows the
// modification time is not necessarily the one the writer intended.
func (s *Store) markLoadedLocked() {
	fi, err := os.Stat(s.indexPath())
	if err != nil {
		return
	}
	s.loadedMod = true
	s.loadedAt = fi.ModTime()
	s.loadedSize = fi.Size()
}

// Settings returns a copy of the persisted configuration.
//
// It reloads first, so a caller in another process (the studio editing the library
// while the pet runs) sees the current state rather than what was on disk at
// startup.
func (s *Store) Settings() Settings {
	s.reloadIfChanged()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.set
	out.Roles = append([]Role(nil), s.set.Roles...)
	out.Lines = make(map[Line]LineSlot, len(s.set.Lines))
	for k, v := range s.set.Lines {
		v.Takes = append([]Take(nil), v.Takes...)
		out.Lines[k] = v
	}
	return out
}

// Roles returns every configured role.
func (s *Store) Roles() []Role {
	s.reloadIfChanged()
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Role(nil), s.set.Roles...)
}

// Role returns one role by ID.
func (s *Store) Role(id string) (Role, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.set.Roles {
		if r.ID == id {
			return r, true
		}
	}
	return Role{}, false
}

// ErrRoleNotFound reports an active role id that names no configured role.
//
// It is an error rather than a degraded answer because the two are not the same
// thing: a missing role means the configuration is inconsistent, and silently
// substituting a different character is how the user came to hear one voice while
// the panel showed another.
var ErrRoleNotFound = errors.New("该角色不存在（可能已被删除）")

// ResolveCurrentRole is THE answer to "which character is in effect".
//
// It is the single resolution entry point for both halves of the feature: the pet
// resolves what to play through it, and the studio resolves what to show and what
// to render through it. They used to consult two unrelated sources - the persisted
// ActiveRole for rendering, and each line's Chosen pointer for playback - which is
// why selecting a character could change the UI without changing a single sound.
//
// The three cases are exhaustive and none of them guesses:
//
//   - ActiveRole empty      -> the built-in sentinel. "Never set" is a real state
//     and it is the ONLY one that legitimately means the embedded clips.
//   - ActiveRole names a role -> that role.
//   - ActiveRole names nothing -> ErrRoleNotFound. Deliberately NOT a fallback to
//     Roles[0]: quietly rendering as some other character is exactly the
//     "selected character and runtime voice are disconnected" failure this replaced.
//
// It reloads first, because the studio is a separate process and a stale answer
// here is a wrong sound rather than a stale number on screen.
func (s *Store) ResolveCurrentRole() (Role, error) {
	s.reloadIfChanged()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resolveCurrentRoleLocked()
}

// resolveCurrentRoleLocked is ResolveCurrentRole for callers already holding s.mu.
//
// A separate method rather than a lock-dropping call inside LineStates, which would
// release and retake the lock mid-snapshot and let a concurrent writer land between
// the role it resolved and the lines it described.
func (s *Store) resolveCurrentRoleLocked() (Role, error) {
	id := s.set.ActiveRole
	if id == "" {
		return BuiltInRoleSentinel(), nil
	}
	for _, r := range s.set.Roles {
		if r.ID == id {
			return r, nil
		}
	}
	return Role{}, fmt.Errorf("%w：%s", ErrRoleNotFound, id)
}

// UpsertRole adds or replaces a role.
//
// Roles are keyed by ID, so editing a role's sampling parameters keeps every take
// already rendered with it - which matters, because tuning against existing takes
// is the whole workflow.
func (s *Store) UpsertRole(r Role) error {
	// Reload first: the studio is a separate process and may have written since
	// we last read, and appending to a stale copy would reuse a version number and
	// overwrite another take's audio.
	s.reloadIfChanged()
	r.Normalise()
	if err := r.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.set.Roles {
		if s.set.Roles[i].ID == r.ID {
			s.set.Roles[i] = r
			return s.save()
		}
	}
	s.set.Roles = append(s.set.Roles, r)
	// ActiveRole is left alone when it is empty. It used to be auto-filled with
	// Roles[0], which made "the user has not chosen yet" and "the user chose the
	// first role" indistinguishable - and the first is the only state that may play
	// the built-in clips.
	return s.save()
}

// DeleteRole removes a role.
//
// Takes rendered with it are KEPT and stay playable: they are already audio, they
// are what the user chose, and losing them because the character's configuration
// was tidied up would be a surprising and irreversible loss.
func (s *Store) DeleteRole(id string) error {
	// Reload first: the studio is a separate process and may have written since
	// we last read, and appending to a stale copy would reuse a version number and
	// overwrite another take's audio.
	s.reloadIfChanged()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.set.Roles[:0]
	for _, r := range s.set.Roles {
		if r.ID != id {
			out = append(out, r)
		}
	}
	s.set.Roles = out
	// Deleting the active role leaves ActiveRole EMPTY rather than pointing at
	// Roles[0]. Empty means "never set", which resolves to the built-in clips - the
	// honest outcome after the character the user picked is gone. Auto-selecting the
	// next role would keep playing a character they did not choose.
	if s.set.ActiveRole == id {
		s.set.ActiveRole = ""
	}
	return s.save()
}

// SetActiveRole makes a role the current character.
//
// The id is VALIDATED before it is stored. Writing an id that names no role used to
// be accepted silently, which deferred the failure to playback where it surfaced as
// the wrong voice rather than as an error - the user-visible form of this whole bug.
//
// Setting the built-in sentinel (or the empty string, its "never set" form) is
// allowed: it is how a caller returns to the embedded clips, and it can never fail.
func (s *Store) SetActiveRole(id string) error {
	// Reload first: the studio is a separate process and may have written since
	// we last read, and appending to a stale copy would reuse a version number and
	// overwrite another take's audio.
	s.reloadIfChanged()
	s.mu.Lock()
	defer s.mu.Unlock()

	if IsBuiltInRole(id) {
		id = ""
	}
	if id != "" {
		found := false
		for _, r := range s.set.Roles {
			if r.ID == id {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%w：%s", ErrRoleNotFound, id)
		}
	}
	s.set.ActiveRole = id
	return s.save()
}

// SeedRoles adds any role that is not already configured, leaving existing ones
// untouched.
//
// Used on first run and by the panel's rescan, so a role the user has already
// tuned is never overwritten by re-reading the models directory. Returns how many
// were added.
func (s *Store) SeedRoles(roles []Role) (int, error) {
	// Reload first: the studio is a separate process and may have written since
	// we last read, and appending to a stale copy would reuse a version number and
	// overwrite another take's audio.
	s.reloadIfChanged()
	s.mu.Lock()
	defer s.mu.Unlock()
	have := map[string]bool{}
	for _, r := range s.set.Roles {
		have[r.ID] = true
	}
	added := 0
	for _, r := range roles {
		r.Normalise()
		if r.ID == "" || have[r.ID] {
			continue
		}
		s.set.Roles = append(s.set.Roles, r)
		have[r.ID] = true
		added++
	}
	if added == 0 {
		return 0, nil
	}
	// ActiveRole is deliberately NOT auto-filled here either; see UpsertRole. A role
	// appearing on disk is not the user choosing it.
	return added, s.save()
}

// lineDir is where a line's takes are stored.
func (s *Store) lineDir(line Line) string {
	return filepath.Join(s.dir, clipsDirName, string(line))
}

// TakeFilePath returns the path of one take's audio, whether or not it exists.
//
// Exported so tests and diagnostics can locate a take without re-deriving the
// directory layout, which is how the earlier in-package test built a wrong path by
// joining SubdirName onto a directory that already ended in it.
func (s *Store) TakeFilePath(line Line, file string) string {
	return filepath.Join(s.lineDir(line), file)
}

// AddTake stores rendered audio as the next take for a line and returns it.
//
// The clip is written before the index is updated, so an interrupted render leaves
// an unreferenced file (harmless, and overwritten by the next attempt) rather than
// an index entry pointing at a file that does not exist - which would be a silent
// failure at playback time.
func (s *Store) AddTake(line Line, audio []byte, role Role, text string) (Take, error) {
	// Reload first: the studio is a separate process and may have written since
	// we last read, and appending to a stale copy would reuse a version number and
	// overwrite another take's audio.
	s.reloadIfChanged()
	if len(audio) == 0 {
		return Take{}, errors.New("合成返回了空音频")
	}
	dir := s.lineDir(line)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Take{}, fmt.Errorf("创建目录失败：%w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	slot := s.set.Lines[line]
	version := 1
	for _, t := range slot.Takes {
		if t.Version >= version {
			version = t.Version + 1
		}
	}
	file := fmt.Sprintf("v%03d.wav", version)

	tmp := filepath.Join(dir, file+".tmp")
	if err := os.WriteFile(tmp, audio, 0o644); err != nil {
		return Take{}, fmt.Errorf("写入音频失败：%w", err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, file)); err != nil {
		os.Remove(tmp)
		return Take{}, fmt.Errorf("保存音频失败：%w", err)
	}

	take := Take{
		Version:     version,
		Role:        role.ID,
		RoleName:    role.Name,
		Text:        text,
		File:        file,
		Bytes:       int64(len(audio)),
		DurationMS:  wavDurationMS(audio),
		Params:      role,
		GeneratedAt: time.Now(),
	}
	slot.Takes = append(slot.Takes, take)
	// A line with nothing chosen adopts its first take, so rendering the whole set
	// makes the new voice audible immediately without a second step.
	//
	// This adoption rule is DELIBERATE and is not the bug: "生成 ≠ 采用" is what
	// makes it safe to render a bad take, and it is pinned by
	// .dev/check-frontend.cjs. What changes here is only the cache: it is seeded
	// with the take that is actually chosen, tagged with its role, rather than with
	// the take just rendered - the latter would make playback follow a regeneration
	// the user never adopted.
	if slot.Chosen == 0 {
		slot.Chosen = version
	}
	s.set.Lines[line] = slot
	if err := s.save(); err != nil {
		return Take{}, err
	}
	if slot.Chosen == version {
		s.paths[line] = cachedClip{role: role.ID, path: filepath.Join(dir, file)}
	} else {
		delete(s.paths, line)
	}
	return take, nil
}

// Choose points a line at one of its takes.
//
// This is the step that makes the workflow safe: auditioning never changes what the
// pet says, so the user can render and compare freely and commit exactly once, when
// they have something they like.
func (s *Store) Choose(line Line, version int) error {
	// Reload first: the studio is a separate process and may have written since
	// we last read, and appending to a stale copy would reuse a version number and
	// overwrite another take's audio.
	s.reloadIfChanged()
	s.mu.Lock()
	defer s.mu.Unlock()
	slot, ok := s.set.Lines[line]
	if !ok {
		return fmt.Errorf("该台词还没有生成过语音")
	}
	found := false
	for _, t := range slot.Takes {
		if t.Version == version {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("第 %d 版不存在", version)
	}
	slot.Chosen = version
	s.set.Lines[line] = slot
	delete(s.paths, line) // invalidate: the chosen clip just changed
	if err := s.save(); err != nil {
		return err
	}
	return nil
}

// ClearChoice drops a line's choice so it falls back to the embedded clip.
func (s *Store) ClearChoice(line Line) error {
	// Reload first: the studio is a separate process and may have written since
	// we last read, and appending to a stale copy would reuse a version number and
	// overwrite another take's audio.
	s.reloadIfChanged()
	s.mu.Lock()
	defer s.mu.Unlock()
	slot, ok := s.set.Lines[line]
	if !ok {
		return nil
	}
	slot.Chosen = 0
	s.set.Lines[line] = slot
	delete(s.paths, line)
	return s.save()
}

// Reset clears every choice at once, returning the pet to its embedded voice
// without deleting any rendered audio.
//
// This is the escape hatch from a bad session: whatever was rendered stays on disk
// and can be re-chosen later, but the pet stops using it immediately.
func (s *Store) Reset() error {
	// Reload first: the studio is a separate process and may have written since
	// we last read, and appending to a stale copy would reuse a version number and
	// overwrite another take's audio.
	s.reloadIfChanged()
	s.mu.Lock()
	defer s.mu.Unlock()
	for line, slot := range s.set.Lines {
		slot.Chosen = 0
		s.set.Lines[line] = slot
	}
	s.paths = map[Line]cachedClip{}
	return s.save()
}

// DeleteTake removes one take and its file.
//
// Deleting the chosen take clears the choice rather than promoting a neighbour:
// silently switching the pet to a take the user did not pick is worse than having
// it fall back to the embedded clip, which is at least a known quantity.
func (s *Store) DeleteTake(line Line, version int) error {
	// Reload first: the studio is a separate process and may have written since
	// we last read, and appending to a stale copy would reuse a version number and
	// overwrite another take's audio.
	s.reloadIfChanged()
	s.mu.Lock()
	defer s.mu.Unlock()
	slot, ok := s.set.Lines[line]
	if !ok {
		return nil
	}
	kept := slot.Takes[:0]
	removed := ""
	for _, t := range slot.Takes {
		if t.Version == version {
			removed = t.File
			continue
		}
		kept = append(kept, t)
	}
	if removed == "" {
		return nil
	}
	slot.Takes = kept
	if slot.Chosen == version {
		slot.Chosen = 0
	}
	s.set.Lines[line] = slot
	delete(s.paths, line)
	if err := s.save(); err != nil {
		return err
	}
	os.Remove(filepath.Join(s.lineDir(line), removed))
	return nil
}

// ClipSource names where a line's audio came from.
type ClipSource string

const (
	// SourceBuiltIn is the clips baked into the executable. It is reachable ONLY
	// when no character has been chosen, which is the single legitimate form of
	// "the built-in voice is in effect".
	SourceBuiltIn ClipSource = "builtin"
	// SourceUser is a take the user rendered AND adopted for the current character.
	SourceUser ClipSource = "user"
)

// ClipResolution is the single answer to "what plays for this line, and why".
//
// Source and Role travel together so no caller has to re-derive which character is
// in effect: that derivation living in two places is what let the panel and the
// player disagree.
type ClipResolution struct {
	// Path is the clip to play. EMPTY for SourceBuiltIn, where the caller
	// materialises the embedded bytes itself.
	Path string
	// Source is where the audio comes from.
	Source ClipSource
	// Role is the character in effect, sentinel included.
	Role Role
}

// ResolveClip resolves a line to the audio that must play, or reports why it
// cannot.
//
// This is the playback half of ResolveCurrentRole, and it is deliberately the only
// path that decides what the pet says. Its rule is one sentence: play the adopted
// take of the CURRENT character, and say so out loud when there is none.
//
// The three outcomes, none of which silently substitutes a different character:
//
//   - no character chosen  -> the built-in clip (SourceBuiltIn).
//   - character R chosen, R has an adopted take for this line -> that take.
//   - character R chosen, R has no adopted take for this line -> an ERROR.
//
// The last case is the important one. It used to play the embedded 可莉 clip, which
// turned "this character has not recorded this sentence yet" into "you hear a
// different character than the one the UI shows" - the reported bug, and one that
// no error message ever surfaced.
func (s *Store) ResolveClip(line Line) (ClipResolution, error) {
	// Reload first: this is THE read that decides what the pet says, so a stale
	// answer here is a wrong sound. The studio runs as a separate process and can
	// have changed the choice at any moment.
	s.reloadIfChanged()

	s.mu.Lock()
	role, err := s.resolveCurrentRoleLocked()
	if err != nil {
		s.mu.Unlock()
		return ClipResolution{}, err
	}
	if IsBuiltInRole(role.ID) {
		s.mu.Unlock()
		return ClipResolution{Source: SourceBuiltIn, Role: role}, nil
	}
	// The cache is only usable when it was resolved for THIS role; a role switch
	// must never hand back the previous character's clip.
	if c, hit := s.paths[line]; hit && c.path != "" && c.role == role.ID {
		s.mu.Unlock()
		return ClipResolution{Path: c.path, Source: SourceUser, Role: role}, nil
	}

	slot, ok := s.set.Lines[line]
	if !ok || slot.Chosen == 0 {
		s.mu.Unlock()
		return ClipResolution{}, noTakeForRole(role, line)
	}
	// The adopted take must have been rendered BY THIS CHARACTER. Taking whatever
	// version is chosen regardless of who rendered it is the other half of the bug:
	// after switching characters, a line kept playing the old character's take
	// because nothing checked whose voice it was.
	var file string
	for _, t := range slot.Takes {
		if t.Version == slot.Chosen {
			if t.Role != role.ID {
				s.mu.Unlock()
				return ClipResolution{}, fmt.Errorf(
					"%s 采用的是「%s」的配音，不是当前角色「%s」的；点「整套换成这个角色」即可用 %s 重新生成并采用",
					lineLabel(line), t.RoleName, role.Name, role.Name)
			}
			file = t.File
			break
		}
	}
	if file == "" {
		s.mu.Unlock()
		return ClipResolution{}, noTakeForRole(role, line)
	}
	path := filepath.Join(s.lineDir(line), file)
	s.mu.Unlock()

	if _, err := os.Stat(path); err != nil {
		// The index says there is a take but the file is gone. Report it rather
		// than falling back to another character's voice: silence-with-a-reason is
		// diagnosable, the wrong character is not.
		return ClipResolution{}, fmt.Errorf(
			"当前角色「%s」%s 的配音文件不见了（%s），点「整套换成这个角色」可重新生成",
			role.Name, lineLabel(line), filepath.Base(path))
	}
	s.mu.Lock()
	s.paths[line] = cachedClip{role: role.ID, path: path}
	s.mu.Unlock()
	return ClipResolution{Path: path, Source: SourceUser, Role: role}, nil
}

// noTakeForRole is the "this character has not recorded this line yet" message.
//
// It names the LINE and the action that fixes it rather than the state that caused
// it: the user is looking at a pet that will not speak this line, and
// "点「整套换成这个角色」" is the whole answer.
func noTakeForRole(role Role, line Line) error {
	return fmt.Errorf("「%s」还没有%s的配音，点「整套换成这个角色」即可生成并采用", role.Name, lineLabel(line))
}

// lineLabel names a line for a message, falling back to the key for a line that has
// no label so an internal name never leaks into the UI nor gets silently dropped.
func lineLabel(line Line) string {
	if l := Labels[line]; l != "" {
		return l
	}
	return string(line)
}

// TakePath returns the absolute path of one specific take, for auditioning.
func (s *Store) TakePath(line Line, version int) (string, bool) {
	s.mu.Lock()
	slot, ok := s.set.Lines[line]
	if !ok {
		s.mu.Unlock()
		return "", false
	}
	var file string
	for _, t := range slot.Takes {
		if t.Version == version {
			file = t.File
			break
		}
	}
	s.mu.Unlock()
	if file == "" {
		return "", false
	}
	path := filepath.Join(s.lineDir(line), file)
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
}

// LineStates describes every line for the studio UI.
//
// embedOK says which lines the binary actually carries, so the panel can tell
// "using the built-in voice" from "nothing to play at all" - a distinction the
// user needs, since only the first is a working state.
func (s *Store) LineStates(embedOK map[Line]bool) []LineState {
	s.reloadIfChanged()
	s.mu.Lock()
	defer s.mu.Unlock()

	// Resolve the character ONCE, under the same lock as the lines, so the badge
	// cannot describe a role different from the one the lines were read for.
	active, roleErr := s.resolveCurrentRoleLocked()
	builtIn := IsBuiltInRole(active.ID)

	out := make([]LineState, 0, len(AllLines))
	for _, line := range AllLines {
		st := LineState{
			Line:     line,
			Text:     Text(line),
			Label:    Labels[line],
			Embedded: embedOK[line],
		}
		if roleErr != nil {
			st.RoleError = roleErr.Error()
		}
		st.ActiveRole = active.ID
		st.ActiveRoleName = active.Name
		if st.Embedded {
			st.BuiltInRole = BuiltInRole
		}

		slot, ok := s.set.Lines[line]
		if ok {
			st.Takes = append([]Take(nil), slot.Takes...)
			st.Chosen = slot.Chosen
			for _, t := range slot.Takes {
				if t.Version == slot.Chosen {
					st.ChosenRole = t.RoleName
					st.ChosenAt = t.GeneratedAt
					st.DurationMS = t.DurationMS
					st.ChosenRoleID = t.Role
					break
				}
			}
		}

		// The one field the UI can show without any interpretation: what plays now,
		// for the character that is currently in effect. It is computed against the
		// ACTIVE ROLE rather than against whatever take happens to be adopted, which
		// is the difference between a badge that describes the pet and one that
		// describes an unrelated pile of audio.
		switch {
		case roleErr != nil:
			// The persisted character does not exist. Nothing plays, and the panel
			// must say so rather than showing the built-in voice as if it were a
			// deliberate choice.
			st.Effect, st.EffectSource = "角色配置有误", "error"
		case builtIn:
			if st.Embedded {
				st.Effect, st.EffectSource = BuiltInRole, "builtin"
			} else {
				st.Effect, st.EffectSource = "无音频", "none"
			}
		case st.Chosen != 0 && st.ChosenRoleID == active.ID:
			// The adopted take belongs to the current character. Only its FILE can
			// still invalidate it.
			path := filepath.Join(s.lineDir(line), chosenFile(slot))
			if _, err := os.Stat(path); err != nil {
				st.Missing = true
				st.Effect, st.EffectSource = active.Name, "missing"
			} else {
				st.Effect, st.EffectSource = st.ChosenRole, "user"
				st.ChosenForActiveRole = true
				if st.Effect == "" {
					st.Effect = active.Name
				}
			}
		case st.Chosen != 0:
			// Something IS adopted, but it was rendered by a different character -
			// the exact state the reported bug produced, where switching character
			// left the old voice playing. It must be visibly distinguished from
			// "not rendered yet", because the fix differs: this line needs
			// re-adopting, not a first render.
			name := st.ChosenRole
			if name == "" {
				name = st.ChosenRoleID
			}
			st.Effect, st.EffectSource = name, "stale"
		default:
			// A character is chosen but this line has no adopted take at all.
			st.Effect, st.EffectSource = active.Name, "pending"
		}
		out = append(out, st)
	}
	return out
}

// chosenFile returns the file name of a slot's chosen take, or "".
func chosenFile(slot LineSlot) string {
	for _, t := range slot.Takes {
		if t.Version == slot.Chosen {
			return t.File
		}
	}
	return ""
}

// LineState is one row of the voice studio.
type LineState struct {
	Line  Line   `json:"line"`
	Text  string `json:"text"`
	Label string `json:"label"`
	// Embedded reports whether the binary carries a built-in clip for this line.
	Embedded bool `json:"embedded"`
	// BuiltInRole names the character the built-in clip was rendered with, so the
	// UI can label it. Empty when there is no built-in clip.
	BuiltInRole string `json:"builtInRole,omitempty"`
	// Effect is the single unambiguous answer to "what will the pet say for this
	// line right now", expressed as the character in effect.
	//
	// It is resolved against the CURRENT role, not against whichever take happens to
	// be adopted: a row must describe the pet, and describing an unrelated pile of
	// audio is precisely how "I switched character and nothing changed" was
	// presented to the user as normal.
	Effect string `json:"effect"`
	// EffectSource is one of "user" / "builtin" / "pending" / "missing" / "none" /
	// "error", so the UI can style the badge and word it without re-deriving the
	// rule. "pending" is the state that used to be invisible: a character is chosen
	// but has no adopted take for this line yet.
	EffectSource string `json:"effectSource"`
	// ActiveRole and ActiveRoleName are the character in effect for the whole
	// snapshot. Repeated per row because a row is rendered on its own, but computed
	// once under one lock so no two rows can disagree.
	ActiveRole     string `json:"activeRole"`
	ActiveRoleName string `json:"activeRoleName,omitempty"`
	// ChosenForActiveRole is true only when the adopted take belongs to the active
	// character, i.e. when this line will really speak in the current voice.
	ChosenForActiveRole bool `json:"chosenForActiveRole,omitempty"`
	// ChosenRoleID is the role id that rendered the adopted take. Kept beside the
	// display name so the UI can tell "adopted, but by someone else" from "not
	// adopted at all" without comparing names.
	ChosenRoleID string `json:"chosenRoleId,omitempty"`
	// RoleError is set when the persisted active role names no configured role.
	RoleError string `json:"roleError,omitempty"`
	// Takes is every render, oldest first.
	Takes []Take `json:"takes,omitempty"`
	// Chosen is the version in use, 0 when nothing has been adopted for this line.
	//
	// NOTE: it is the LINE's adopted version, which may belong to a character other
	// than the active one. ChosenForActiveRole is what says whether it plays now.
	Chosen int `json:"chosen"`
	// ChosenRole and ChosenAt describe the chosen take.
	ChosenRole string    `json:"chosenRole,omitempty"`
	ChosenAt   time.Time `json:"chosenAt,omitempty"`
	// DurationMS is the chosen take's length.
	DurationMS int `json:"durationMs,omitempty"`
	// Missing is true when a take was chosen but its file is gone, so the panel can
	// say so instead of silently showing the embedded voice.
	Missing bool `json:"missing,omitempty"`
}

// DiscoverRoles finds characters on a local GPT-SoVITS installation.
//
// It scans <ttsDir>/models/*/ for the three files a role needs. This is what makes
// the feature work without a hand-written role list: the models already on this
// machine follow that layout (that is how the previous real-time path used them),
// so the studio can offer them immediately, and dropping in another character's
// folder makes it appear on the next rescan.
//
// A directory missing any of the three files is skipped rather than reported: a
// half-copied model is a transient state during a download, not something the user
// needs to be told about.
func DiscoverRoles(ttsDir string) []Role {
	models := filepath.Join(ttsDir, "models")
	entries, err := os.ReadDir(models)
	if err != nil {
		return nil
	}
	var out []Role
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(models, e.Name())
		gpt := filepath.Join(dir, "gpt.ckpt")
		sov := filepath.Join(dir, "sovits.pth")
		ref := filepath.Join(dir, "ref.wav")
		if !fileExists(gpt) || !fileExists(sov) || !fileExists(ref) {
			continue
		}
		r := Role{
			ID:   slug(e.Name()),
			Name: roleName(dir, e.Name()),
			GPT:  gpt, SoVITS: sov, Ref: ref,
		}
		// The per-character config the previous implementation wrote is reused for
		// the display name and the reference transcript, because it already holds
		// both and re-deriving the name from a directory slug would show "klee"
		// where the user expects 可莉.
		if pc, ok := readPetPCConfig(dir); ok {
			if pc.Character != "" {
				r.Name = pc.Character
			}
			r.PromptText = pc.PromptText
			if pc.Lang != "" {
				r.Lang = pc.Lang
			}
		}
		r.Normalise()
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// petPCConfig is the subset of the old per-character config this needs.
type petPCConfig struct {
	Character  string `json:"ttsCharacter"`
	PromptText string `json:"ttsPromptText"`
	Lang       string `json:"ttsLang"`
}

func readPetPCConfig(dir string) (petPCConfig, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "petpc-config.json"))
	if err != nil {
		return petPCConfig{}, false
	}
	var c petPCConfig
	if err := json.Unmarshal(trimBOM(data), &c); err != nil {
		return petPCConfig{}, false
	}
	return c, true
}

// roleName is the display name for a character directory: its config's name when
// there is one, else the directory name.
func roleName(dir, fallback string) string {
	if c, ok := readPetPCConfig(dir); ok && c.Character != "" {
		return c.Character
	}
	return fallback
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// slug reduces a name to a safe identifier for use as a key and a directory name.
//
// Non-ASCII is preserved rather than stripped, because the character names ARE
// non-ASCII and "可莉" is a far better ID than "". Everything that could confuse a
// file system or a URL is replaced.
func slug(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r > 127:
			// CJK and friends: safe in a file name and readable in the JSON.
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// wavDurationMS measures a clip from its RIFF header.
//
// Measured rather than estimated from the file size: the engine's sample rate is
// not guaranteed, and a length shown in the UI that disagrees with what the clip
// sounds like is worse than no length at all.
func wavDurationMS(data []byte) int {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return 0
	}
	var rate, channels, bits, dataLen int
	for off := 12; off+8 <= len(data); {
		id := string(data[off : off+4])
		size := int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		body := off + 8
		switch id {
		case "fmt ":
			if body+16 <= len(data) {
				channels = int(binary.LittleEndian.Uint16(data[body+2 : body+4]))
				rate = int(binary.LittleEndian.Uint32(data[body+4 : body+8]))
				bits = int(binary.LittleEndian.Uint16(data[body+14 : body+16]))
			}
		case "data":
			dataLen = size
			if body+dataLen > len(data) {
				dataLen = len(data) - body
			}
		}
		// A zero or absurd chunk size would loop forever; both mean a header this
		// parser does not understand, so stop.
		if size <= 0 || body+size > len(data) {
			break
		}
		off = body + size + (size & 1)
	}
	if rate == 0 || channels == 0 || bits == 0 {
		return 0
	}
	bytesPerSec := rate * channels * bits / 8
	if bytesPerSec == 0 {
		return 0
	}
	return dataLen * 1000 / bytesPerSec
}
