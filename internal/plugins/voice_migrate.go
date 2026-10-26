package plugins

// One-shot migration of the host's voice library into the voice plugin.
//
// The pet used to own speech: the lines, the character library and the rendered
// clips all lived under <dataDir>/voice/user/, and the user built that library by
// spending minutes of CPU per line through the standalone voice studio. This change
// moves speech into a plugin, and the plugin's data lives somewhere else - so
// without this migration the audio would still be on disk and nothing would play
// it.
//
// The three properties are the conversation migration's (see MigrateChatData), and
// they matter for the same reasons:
//
//  1. THE SOURCE IS ONLY EVER READ. <dataDir>/voice/ is left exactly as it is, which
//     is what makes a bad migration recoverable: the original renders are still
//     there, and re-running with the marker removed tries again.
//  2. EVERYTHING IS WRITTEN BEFORE ANYTHING IS MARKED. A failure halfway leaves no
//     marker, so the next launch retries rather than shipping a half-populated
//     library that would look like "your voice is gone".
//  3. A MARKER ENDS IT. Its presence means "this already ran", so the source is not
//     re-read on every launch.
//
// WHAT IS DELIBERATELY NOT DONE
//
// The source is NOT renamed, moved or deleted - unlike the conversation migration,
// which renames its sources to .bak. The difference is what the data costs to
// recreate: a chat history can be rebuilt by talking, while these clips took
// minutes of GPU time EACH and need a running GPT-SoVITS service to make again.
// The disk they occupy is a few megabytes; the cost of guessing wrong about them is
// not recoverable. So they stay, and the migration copies.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// VoicePluginID is the plugin this migration populates.
const VoicePluginID = "com.shixiansi.plugin-voice"

// voiceMigrateDir is the legacy library's home, relative to the data directory.
const voiceMigrateDir = "voice"

// voiceStorageKey is the plugin's settings slot.
//
// It must match HISTORY_KEY in the plugin's app.js: the plugin reads its switches
// and its chosen takes from this one key, so a migration that wrote a different name
// would populate the disk and change nothing the user can hear.
const voiceStorageKey = "voice-settings"

// legacyVoiceLibrary is the shape of <dataDir>/voice/user/voice-user.json.
//
// It is declared HERE rather than by importing the old package because that package
// is being deleted, and a migration that depended on it would have to be deleted
// with it - leaving every existing user's library unmigrated. Only the fields this
// migration reads are declared; the old file has more, and unknown fields are
// ignored rather than preserved.
type legacyVoiceLibrary struct {
	ActiveRole string                     `json:"activeRole"`
	Roles      []legacyVoiceRole          `json:"roles"`
	Lines      map[string]legacyVoiceSlot `json:"lines"`
}

type legacyVoiceRole struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	GPT    string `json:"gpt"`
	SoVITS string `json:"sovits"`
	Ref    string `json:"ref"`
}

type legacyVoiceSlot struct {
	Takes []legacyVoiceTake `json:"takes"`
	// Chosen is 1-BASED: it indexes Takes as `Takes[Chosen-1]`, which is how the
	// studio wrote it. A zero means "nothing chosen".
	Chosen int `json:"chosen"`
}

type legacyVoiceTake struct {
	Version    int    `json:"version"`
	Role       string `json:"role"`
	RoleName   string `json:"roleName"`
	Text       string `json:"text"`
	File       string `json:"file"`
	Bytes      int64  `json:"bytes"`
	DurationMS int    `json:"durationMs"`
}

// voicePluginSettings is what the plugin's app.js expects at voiceStorageKey.
//
// It is a struct rather than a map so the field names are checked by the compiler
// against the JSON the plugin parses; a typo here would produce a library that loads
// and silently ignores everything in it.
type voicePluginSettings struct {
	Enabled    bool   `json:"enabled"`
	Pomodoro   bool   `json:"pomodoro"`
	Reminder   bool   `json:"reminder"`
	ActiveRole string `json:"activeRole"`
	// Chosen maps a line id to the audio slot name in use.
	Chosen map[string]string `json:"chosen"`
	// Takes maps a line id to every render this plugin holds for it, with the
	// metadata the host's audio.list cannot know: which character, which sentence,
	// how long. It is what lets the workshop offer "switch every line to 宵宫"
	// without an engine.
	Takes map[string][]voicePluginTake `json:"takes"`
	// RoleNames maps a role id to its display name.
	RoleNames map[string]string `json:"roleNames"`
	// Imported records that the packaged built-in clips were copied in, so the
	// plugin does not repeat it.
	Imported bool `json:"imported"`
}

type voicePluginTake struct {
	Name       string `json:"name"`
	Role       string `json:"role"`
	RoleName   string `json:"roleName"`
	Text       string `json:"text"`
	DurationMS int    `json:"durationMs"`
	Bytes      int64  `json:"bytes"`
}

// MigrateVoiceData copies the legacy voice library into the voice plugin's private
// directory.
//
// It reports no error when there is nothing to do: "the plugin is not installed",
// "the library was never created" and "this already ran" are ordinary states. An
// error means the data was there and could not be copied, and in that case the
// plugin's directory is left without a marker so the next launch retries.
//
// `keeper` encrypts secrets for the config store. The voice plugin declares none today,
// so it is unused in practice - it is threaded through because the store REQUIRES one to
// be able to write at all, and passing nil would make this fail the day the plugin
// declares its first secret.
func MigrateVoiceData(dataDir string, keeper SecretKeeper) error {
	if strings.TrimSpace(dataDir) == "" {
		return errors.New("迁移缺少数据目录")
	}
	privateDir, man, ok := voicePluginOnDisk(dataDir)
	if !ok {
		// Not installed yet. No marker: the next launch looks again, which is what
		// makes "install the plugin, then restart the pet" work.
		return nil
	}
	if markerExists(privateDir) {
		return nil
	}

	legacy, err := readLegacyVoiceLibrary(dataDir)
	if err != nil {
		return err
	}
	if legacy == nil {
		// No legacy library at all. Nothing to carry, and nothing to mark: the user
		// may still create one in an older build, and marking here would skip it.
		return nil
	}

	audioDir := filepath.Join(privateDir, "audio")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		return fmt.Errorf("创建语音目录失败：%w", err)
	}

	// The switches came from the pet's config.json. They are read defensively: a
	// missing config.json leaves the defaults, which is the same state a fresh
	// install gets.
	cfg := legacyVoiceSwitches(dataDir)

	out := voicePluginSettings{
		Enabled:    cfg.enabled,
		Pomodoro:   cfg.pomodoro,
		Reminder:   cfg.reminder,
		ActiveRole: legacy.ActiveRole,
		Chosen:     map[string]string{},
		Takes:      map[string][]voicePluginTake{},
		RoleNames:  map[string]string{},
	}

	for _, role := range legacy.Roles {
		if role.ID != "" {
			out.RoleNames[role.ID] = role.Name
		}
	}

	// Copy every take this library holds, keeping the character each belongs to.
	//
	// EVERY take is copied, not only the active character's: the workshop's whole
	// purpose is switching between characters, and a migration that kept just one
	// would destroy the others' renders - which is the exact loss this function
	// exists to prevent.
	lineIDs := make([]string, 0, len(legacy.Lines))
	for lineID := range legacy.Lines {
		lineIDs = append(lineIDs, lineID)
	}
	// Sorted so the migration is deterministic: a map's order is random, and a test
	// (or a re-run) that produced different slot names each time would be useless
	// for comparison.
	sort.Strings(lineIDs)

	for _, lineID := range lineIDs {
		slot := legacy.Lines[lineID]
		// copied records which takes actually made it to disk, because the CHOICE
		// below must only ever name a file that exists. A library that names a clip
		// which is gone (deleted by hand, or a half-finished render) would otherwise
		// migrate to a plugin that points every event at a missing file - which plays
		// NOTHING, silently, and looks exactly like the feature being broken.
		copied := make(map[int]bool, len(slot.Takes))
		for i, take := range slot.Takes {
			if take.File == "" {
				continue
			}
			src := filepath.Join(dataDir, voiceMigrateDir, "user", "clips", lineID, filepath.Base(take.File))
			raw, err := os.ReadFile(src)
			if err != nil {
				// A clip named in the JSON but absent on disk is a broken library,
				// not a reason to abandon the rest: the other lines still play. It
				// is reported on stderr rather than failing, because failing here
				// would leave the user with NOTHING migrated over one missing file.
				fmt.Fprintf(os.Stderr, "voice 迁移：跳过缺失的片段 %s：%v\n", src, err)
				continue
			}
			name := voiceSlotName(lineID, take)
			if err := atomicWriteFile(filepath.Join(audioDir, name+".wav"), raw); err != nil {
				return fmt.Errorf("迁移语音片段 %s 失败：%w", name, err)
			}
			copied[i] = true
			out.Takes[lineID] = append(out.Takes[lineID], voicePluginTake{
				Name:       name,
				Role:       take.Role,
				RoleName:   take.RoleName,
				Text:       take.Text,
				DurationMS: take.DurationMS,
				Bytes:      int64(len(raw)),
			})
		}
		// Which take is in effect for this line.
		//
		// The 1-based index is honoured when it names a take of the ACTIVE
		// character AND that take was actually copied, because that is literally what
		// the pet is playing today. Otherwise the first copied take of the active
		// character is used - which is what the user believes they are hearing.
		if name := chosenSlotFor(lineID, slot, legacy.ActiveRole, copied); name != "" {
			out.Chosen[lineID] = name
		}
	}

	// The settings go in LAST among the data, so a failure above leaves the plugin
	// with clips but no pointer at them rather than the reverse - and no marker
	// either way, so it retries.
	encoded, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("编码语音设置失败：%w", err)
	}
	if err := atomicWriteFile(filepath.Join(privateDir, voiceStorageKey), encoded); err != nil {
		return fmt.Errorf("写入语音设置失败：%w", err)
	}

	// The three weight paths follow the library, so the workshop can RENDER from the
	// moment it is installed.
	//
	// They are worth carrying even though the clips themselves are already here, and the
	// reason is what the user would otherwise face: a plugin holding 12 playable clips
	// and a BLANK 生成 form, with no record anywhere of which gpt/sovits/ref produced
	// them. The old library knew - every take carried its `params` - so dropping them
	// would throw away the only copy of "which voice these came from", and the user would
	// have to retype three paths per character from memory to render one more line.
	//
	// Only the ACTIVE character's paths go in, because the plugin's config declares
	// exactly one set. That is the character whose clips were adopted, so the form starts
	// out consistent with what plays.
	if err := writeVoiceConfig(privateDir, man, legacy, keeper); err != nil {
		// Not fatal to the migration: the clips and the switches are already in place and
		// reachable, so failing here would trade a fully working playback library for a
		// blank 生成 form. It is reported instead, and the marker still gets written -
		// otherwise every launch would retry a step that cannot succeed.
		fmt.Fprintf(os.Stderr, "voice 迁移：写入引擎配置失败（生成界面需要手工填写三个路径）：%v\n", err)
	}

	// The marker ends it. Its content is a timestamp and what was carried, so the
	// file answers "did this run, and what did it take?".
	marker := fmt.Sprintf("%s\n%d lines, %d clips\n",
		time.Now().Format(time.RFC3339), len(out.Chosen), countTakes(out.Takes))
	return atomicWriteFile(filepath.Join(privateDir, migrateMarkerName), []byte(marker))
}

// writeVoiceConfig seeds the plugin's declared settings from the legacy library.
//
// It goes through ConfigStore rather than writing JSON by hand, and that is the point
// rather than tidiness: ConfigStore owns the file's shape (`{"values":{...}}`), refuses
// undeclared keys, merges over what is already there, and writes atomically. A
// hand-rolled writer would have to reproduce all four, and the first thing it got wrong
// when this was drafted was the file's shape - it invented a `secrets` object that the
// format does not have.
//
// The manifest must be the plugin's REAL one, because the store drops any key the
// manifest does not declare: that is what makes "the keys here are the form the user
// sees" true by construction rather than by inspection.
func writeVoiceConfig(privateDir string, man Manifest, legacy *legacyVoiceLibrary, keeper SecretKeeper) error {
	role, ok := activeLegacyRole(legacy)
	if !ok {
		// No active character, or the one named is not in the library. Nothing useful to
		// write, and writing blanks would be indistinguishable from "the user cleared
		// them".
		return nil
	}

	values := map[string]string{}
	if role.GPT != "" {
		values["gptPath"] = role.GPT
	}
	if role.SoVITS != "" {
		values["sovitsPath"] = role.SoVITS
	}
	if role.Ref != "" {
		values["refPath"] = role.Ref
	}
	if len(values) == 0 {
		return nil
	}

	store := NewConfigStore(privateDir, man, keeper)
	// MERGED, not overwritten: a config.json may already exist (the user edited the form
	// before the migration ran), and a value the user typed wins over one inferred from
	// the old library.
	for k := range store.Values() {
		delete(values, k)
	}
	if len(values) == 0 {
		return nil
	}
	return store.Write(values)
}

// activeLegacyRole finds the character the old library was speaking as.
func activeLegacyRole(legacy *legacyVoiceLibrary) (legacyVoiceRole, bool) {
	for _, r := range legacy.Roles {
		if r.ID == legacy.ActiveRole {
			return r, true
		}
	}
	return legacyVoiceRole{}, false
}

// voiceSlotName builds the audio slot name for one take.
//
// The name is the join key between three places that must agree: the file on disk,
// the plugin's `chosen` value and the plugin's `takes` metadata. It uses the same
// character set the host's audio capability accepts ([A-Za-z0-9_-]) because a name
// outside it would be refused at play time - the migration would succeed and nothing
// would ever sound.
func voiceSlotName(lineID string, take legacyVoiceTake) string {
	version := take.Version
	if version <= 0 {
		// A take with no version still needs a stable, legal name. The file name is
		// the fallback, sanitised, so a re-run over the same library produces the
		// same names.
		base := strings.TrimSuffix(take.File, filepath.Ext(take.File))
		return sanitiseSlot(lineID + "-" + base)
	}
	return sanitiseSlot(fmt.Sprintf("%s-v%03d", lineID, version))
}

// sanitiseSlot reduces a string to the characters the audio capability accepts.
func sanitiseSlot(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

// chosenSlotFor decides which copied take the active character uses for one line.
//
// The line id is a parameter rather than derived from the slot, because the slot
// does not carry one: the slot name is built FROM the line id, and passing it in is
// what keeps the two consistent.
//
// `copied` is indexed by the position in slot.Takes and says which takes reached the
// disk. A choice may only name a copied take: naming a missing file would migrate to
// a plugin that plays nothing at all, which is worse than playing the wrong take
// because it is silent and looks like a broken feature.
func chosenSlotFor(lineID string, slot legacyVoiceSlot, activeRole string, copied map[int]bool) string {
	// The index wins when it points at the active character AND that take made it.
	if slot.Chosen > 0 && slot.Chosen <= len(slot.Takes) && copied[slot.Chosen-1] {
		take := slot.Takes[slot.Chosen-1]
		if activeRole == "" || take.Role == activeRole {
			return voiceSlotName(lineID, take)
		}
	}
	// Otherwise the first COPIED take of the active character.
	for i, take := range slot.Takes {
		if !copied[i] {
			continue
		}
		if take.Role == activeRole {
			return voiceSlotName(lineID, take)
		}
	}
	return ""
}

// countTakes totals the copied renders, for the marker.
func countTakes(takes map[string][]voicePluginTake) int {
	n := 0
	for _, list := range takes {
		n += len(list)
	}
	return n
}

// readLegacyVoiceLibrary reads voice-user.json, or returns nil when it is absent.
func readLegacyVoiceLibrary(dataDir string) (*legacyVoiceLibrary, error) {
	path := filepath.Join(dataDir, voiceMigrateDir, "user", "voice-user.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取语音库失败：%w", err)
	}
	var lib legacyVoiceLibrary
	if err := json.Unmarshal(raw, &lib); err != nil {
		// A library that cannot be parsed is reported rather than skipped: silently
		// producing an empty plugin would look exactly like "the migration worked
		// and your voice is gone".
		return nil, fmt.Errorf("解析语音库失败：%w", err)
	}
	if len(lib.Lines) == 0 && lib.ActiveRole == "" {
		return nil, nil
	}
	return &lib, nil
}

// legacyVoiceSwitches reads the three switches the pet used to hold.
//
// It is deliberately tolerant: the pet's config.json is read as a loose map rather
// than through the store package, because the fields it names are being DELETED in
// this change and a migration that imported them could not survive that. A missing
// or unreadable file yields the defaults.
func legacyVoiceSwitches(dataDir string) struct{ enabled, pomodoro, reminder bool } {
	// The defaults match the pet's: speech off, pomodoro lines on, reminder lines
	// off. Using them when the file cannot be read keeps the migrated state equal to
	// what a user who never touched the switches had.
	out := struct{ enabled, pomodoro, reminder bool }{false, true, false}

	raw, err := os.ReadFile(filepath.Join(dataDir, "config.json"))
	if err != nil {
		return out
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return out
	}
	if v, ok := doc["voiceEnabled"].(bool); ok {
		out.enabled = v
	}
	if v, ok := doc["voicePomodoro"].(bool); ok {
		out.pomodoro = v
	}
	if v, ok := doc["voiceReminder"].(bool); ok {
		out.reminder = v
	}
	return out
}

// voicePluginOnDisk finds the voice plugin's installed manifest, and prepares its
// private directory.
//
// The plugin directory and its private directory are NOT created together: `Install`
// unpacks the package and records the index entry, but nothing makes a private
// directory until something first writes to one. So this used to require both to
// exist, and the requirement was wrong in a way that was easy to miss - a user who
// installed the plugin and restarted the pet got NO migration, silently, because the
// private directory had not been touched yet. It only appeared to work for anyone who
// had already opened the plugin's workshop (which writes its settings) or whose
// installer had created the directory for them.
//
// The private directory is therefore CREATED here rather than required. That is safe:
// the plugin is installed and its manifest parses, so this is a directory the plugin
// will use anyway, and the caller is about to write a library into it. A MANIFEST that
// cannot be read still refuses, because that is not an install this migration could
// complete correctly.
//
// The MANIFEST comes back with the directory because the config seed needs it: the
// config store drops every key the manifest does not declare, so writing the three
// weight paths without it would silently store nothing.
func voicePluginOnDisk(dataDir string) (privateDir string, man Manifest, ok bool) {
	manifestPath := filepath.Join(dataDir, "plugins", VoicePluginID, ManifestName)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", Manifest{}, false
	}
	parsed, err := ParseManifest(raw)
	if err != nil || parsed.ID != VoicePluginID {
		return "", Manifest{}, false
	}
	private := filepath.Join(dataDir, "plugin-data", VoicePluginID)
	if err := os.MkdirAll(private, 0o755); err != nil {
		return "", Manifest{}, false
	}
	return private, parsed, true
}

// VoiceMigrateMarkerPath returns where the voice migration's marker lives.
func VoiceMigrateMarkerPath(dataDir string) string {
	return filepath.Join(dataDir, "plugin-data", VoicePluginID, migrateMarkerName)
}
