package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// This file owns a plugin's DECLARED SETTINGS: the values it asked for in its
// manifest, kept in the plugin's own private directory.
//
// Three rules shape everything here:
//
//  1. Values live in <privateDir>/config.json and secrets in
//     <privateDir>/secret-<key>.bin, i.e. inside the directory that is deleted
//     when the plugin is uninstalled. Leaving settings behind would be state
//     nothing can ever read or clean up again.
//  2. A secret's plaintext NEVER leaves Go in a form a caller can print: reads
//     return {set, mask} and nothing else, and a write with no SecretKeeper
//     injected is refused rather than quietly falling back to plaintext.
//  3. Only keys the plugin actually declared may be written. A store that
//     accepted arbitrary keys would let a plugin (or a bug) accumulate settings
//     its own manifest does not describe, and no editor would ever show them.

// SecretKeeper is the host's encrypted-at-rest secret storage.
//
// It is an INTERFACE rather than a direct use of internal/desktop's
// SecretStore, so this package keeps the boundary its file header declares: it
// uses the standard library only and never imports internal/desktop. Both
// programs that need it (the pet and the plugin market) inject the same
// implementation.
type SecretKeeper interface {
	// Get returns the plaintext at path, or "" when nothing is stored there.
	Get(path string) (string, error)
	// Set encrypts value and writes it to path.
	Set(path, value string) error
	// Clear removes whatever is stored at path.
	Clear(path string) error
}

// ConfigItemState describes a secret without revealing it.
type ConfigItemState struct {
	// Set is true when a usable secret is stored.
	Set bool `json:"set"`
	// Mask is a reminder of which secret is in use, never a way to read it.
	Mask string `json:"mask,omitempty"`
}

// ConfigValues is one plugin's settings as a UI may see them.
//
// Secrets appear in Secrets and NEVER in Values, which is what makes "print the
// whole struct" a safe operation.
type ConfigValues struct {
	Values  map[string]string          `json:"values"`
	Secrets map[string]ConfigItemState `json:"secrets"`
}

// configFile is the on-disk shape of config.json.
//
// The values are wrapped in an object rather than written as a bare map so the
// file has room to grow (a version, a note) without a migration.
type configFile struct {
	Values map[string]string `json:"values"`
}

// ConfigStore persists ONE plugin's declared settings.
type ConfigStore struct {
	dir      string
	manifest Manifest
	keeper   SecretKeeper
}

// NewConfigStore returns the store for one plugin's private directory.
//
// keeper may be nil, in which case the plugin cannot store secrets at all - the
// write is refused rather than downgraded, because a plaintext credential on
// disk is the one outcome this type exists to prevent.
func NewConfigStore(privateDir string, man Manifest, keeper SecretKeeper) *ConfigStore {
	return &ConfigStore{dir: privateDir, manifest: man, keeper: keeper}
}

// Dir returns the directory the settings live in, for diagnostics.
func (c *ConfigStore) Dir() string { return c.dir }

// configPath is the non-secret settings file.
func (c *ConfigStore) configPath() string { return filepath.Join(c.dir, "config.json") }

// secretPath is where one secret item is stored.
//
// The key has already been validated against configKeyPattern by the manifest
// parser, so it cannot contain a separator; the join therefore cannot escape the
// private directory.
func (c *ConfigStore) secretPath(key string) string {
	return filepath.Join(c.dir, "secret-"+key+".bin")
}

// Read returns the stored settings: non-secret values verbatim and secret items
// as {set, mask}.
//
// A secret that cannot be decrypted (a different Windows user, a restored
// backup) is reported as "not set" rather than as an error, matching
// internal/desktop's position: from the user's point of view that credential
// simply is not usable, and the editor should offer to re-enter it.
func (c *ConfigStore) Read() (ConfigValues, error) {
	raw, err := c.readFile()
	if err != nil {
		return ConfigValues{}, err
	}
	out := ConfigValues{Values: map[string]string{}, Secrets: map[string]ConfigItemState{}}
	for _, item := range c.manifest.Config {
		if item.Secret {
			out.Secrets[item.Key] = c.secretState(item.Key)
			continue
		}
		if v, ok := raw[item.Key]; ok {
			out.Values[item.Key] = v
		}
	}
	return out, nil
}

// secretState reports whether a secret is stored and how it masks.
func (c *ConfigStore) secretState(key string) ConfigItemState {
	if c.keeper == nil {
		return ConfigItemState{}
	}
	value, err := c.keeper.Get(c.secretPath(key))
	if err != nil || strings.TrimSpace(value) == "" {
		return ConfigItemState{}
	}
	return ConfigItemState{Set: true, Mask: MaskSecret(value)}
}

// PlainSecret returns one declared secret's plaintext for a CALLER INSIDE Go
// that needs to use it (the llm.chat capability builds a request with it).
//
// It is deliberately not part of Read: everything that crosses the bridge goes
// through Read, so there is exactly one shape a UI can receive and that shape
// has no plaintext in it.
func (c *ConfigStore) PlainSecret(key string) string {
	item, ok := c.manifest.ConfigItem(key)
	if !ok || !item.Secret || c.keeper == nil {
		return ""
	}
	value, err := c.keeper.Get(c.secretPath(key))
	if err != nil {
		return ""
	}
	return value
}

// Value returns one declared non-secret setting, or "" when it is unset.
func (c *ConfigStore) Value(key string) string {
	item, ok := c.manifest.ConfigItem(key)
	if !ok || item.Secret {
		return ""
	}
	raw, err := c.readFile()
	if err != nil {
		return ""
	}
	return raw[key]
}

// Values returns every stored non-secret setting.
func (c *ConfigStore) Values() map[string]string {
	raw, err := c.readFile()
	if err != nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if item, ok := c.manifest.ConfigItem(k); ok && !item.Secret {
			out[k] = v
		}
	}
	return out
}

// Write stores the given settings, merging them into what is already there.
//
// Every key is validated BEFORE anything is written, so a request carrying one
// undeclared key changes nothing at all - the same all-or-nothing rule the
// permission check follows.
//
// A secret item with a non-empty value is encrypted and written to its own file;
// an empty value CLEARS it. The empty string is never stored as a secret.
func (c *ConfigStore) Write(values map[string]string) error {
	// 1. Validate everything first.
	for key := range values {
		if _, ok := c.manifest.ConfigItem(key); !ok {
			return fmt.Errorf("插件未声明配置项 %s", key)
		}
	}
	// 2. Secrets first: if one cannot be stored (no keeper, a crypto failure),
	//    the non-secret file has not been touched yet.
	for key, value := range values {
		item, _ := c.manifest.ConfigItem(key)
		if !item.Secret {
			continue
		}
		if c.keeper == nil {
			return errors.New("无法保存密钥：本进程没有注入密钥存储")
		}
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			if err := c.keeper.Clear(c.secretPath(key)); err != nil {
				return fmt.Errorf("清除密钥 %s 失败：%w", key, err)
			}
			continue
		}
		if err := c.keeper.Set(c.secretPath(key), trimmed); err != nil {
			return fmt.Errorf("保存密钥 %s 失败：%w", key, err)
		}
	}
	// 3. Then the plain values, in one atomic replace.
	raw, err := c.readFile()
	if err != nil {
		return err
	}
	for key, value := range values {
		item, _ := c.manifest.ConfigItem(key)
		if item.Secret {
			continue
		}
		raw[key] = normaliseConfigValue(item, value)
	}
	return c.writeFile(raw)
}

// readFile loads config.json, treating a missing or unreadable file as empty.
//
// A corrupt file is NOT quarantined here the way the plugin index is: the file
// is written by this package alone, it is small and regenerable, and a .bak of
// half a settings form helps nobody. It is simply ignored, and the next write
// replaces it.
func (c *ConfigStore) readFile() (map[string]string, error) {
	data, err := os.ReadFile(c.configPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("读取插件配置失败：%w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]string{}, nil
	}
	var parsed configFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return map[string]string{}, nil
	}
	if parsed.Values == nil {
		return map[string]string{}, nil
	}
	return parsed.Values, nil
}

// writeFile atomically replaces config.json.
func (c *ConfigStore) writeFile(values map[string]string) error {
	if values == nil {
		values = map[string]string{}
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return fmt.Errorf("创建插件私有目录失败：%w", err)
	}
	data, err := json.MarshalIndent(configFile{Values: values}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWriteFile(c.configPath(), data)
}

// normaliseConfigValue renders one value for storage.
//
// Only bool has a canonical form worth enforcing: "true"/"false" is what the
// editor's checkbox sends and what a plugin comparing the string expects. Every
// other type is stored exactly as the user typed it - the manifest's Type is a
// control hint, not a coercion promise.
func normaliseConfigValue(item ConfigItem, value string) string {
	v := strings.TrimSpace(value)
	if item.Type == "bool" {
		switch strings.ToLower(v) {
		case "true", "1", "yes", "on":
			return "true"
		default:
			return "false"
		}
	}
	return v
}

// MaskSecret renders a secret for display, revealing at most the last four
// characters.
//
// It mirrors internal/desktop's SecretStore.Mask on purpose: a user who has seen
// one mask in this app should recognise the other. Anything shorter than 16
// characters is hidden completely - revealing 7 of a 9-character key gives most
// of it away, and a mask has to be a reminder, not a partial disclosure.
func MaskSecret(value string) string {
	if value == "" {
		return ""
	}
	if len(value) < 16 {
		return "••••"
	}
	return value[:3] + "…" + value[len(value)-4:]
}

// ConfigHosts returns the extra network hostnames a plugin's settings supply.
//
// Only the single HostWhitelist item contributes, and its value may be a
// comma/whitespace separated list. Each segment is either a bare host (which
// must match netHostPattern) or a URL whose scheme must be http/https/ws/wss, in
// which case its host is used.
//
// ws/wss are accepted alongside http/https because the same setting is what
// points a net.ws plugin at its service: the address a user types is one string,
// and refusing to let a `ws://` value contribute its host would leave the
// WebSocket capability unable to reach the very endpoint the form asks for.
// The scheme check is not a policy boundary - HostAllowed compares the host, and
// each capability checks its own scheme again before dialling or fetching.
//
// Two properties are deliberate:
//
//   - A segment carrying "*" contributes NOTHING. The allow-list is exact
//     (HostAllowed compares whole hosts), and a wildcard that was silently
//     accepted would read as a broad grant while matching nothing - or worse,
//     invite a later "fix" that made it a real wildcard.
//   - A malformed segment contributes nothing rather than failing the call: the
//     address a user typed is edited in a form, and a half-typed endpoint must
//     not turn every net.fetch into an error. What it can never do is widen the
//     allow-list.
func ConfigHosts(man Manifest, values map[string]string) []string {
	var out []string
	for _, item := range man.Config {
		if !item.HostWhitelist {
			continue
		}
		raw, ok := values[item.Key]
		if !ok {
			continue
		}
		for _, seg := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ';'
		}) {
			seg = strings.TrimSpace(seg)
			if seg == "" || strings.Contains(seg, "*") {
				continue
			}
			if strings.Contains(seg, "://") {
				u, err := url.Parse(seg)
				if err != nil || !hostContributingScheme(u.Scheme) || u.Host == "" {
					continue
				}
				out = append(out, u.Host)
				continue
			}
			if !netHostPattern.MatchString(seg) {
				continue
			}
			out = append(out, seg)
		}
	}
	return out
}

// hostContributingScheme reports whether a URL scheme may contribute its host to
// the allow-list. See ConfigHosts for why ws/wss are included.
func hostContributingScheme(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "http", "https", "ws", "wss":
		return true
	}
	return false
}
