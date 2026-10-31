package plugins

// Model channels: the route table a plugin keeps for itself, and the keys next to it.
//
// # WHY THIS EXISTS
//
// `llm.chat` could only ever use ONE endpoint per plugin: the manifest's `llmBaseUrl` /
// `llmModel` items plus the single `secret` item. That is fine for "paste your key here"
// and useless for the real case this repo hit next: several providers, several models per
// provider, and more than one key per provider (one that is out of quota today, one that
// is not).
//
// A manifest cannot express a list that grows at runtime - the config items are static, and
// ConfigStore refuses undeclared keys on purpose (config.go: "a store that accepted
// arbitrary keys would let a plugin accumulate settings its own manifest does not describe,
// and no editor would ever show them"). So the list lives in TWO files inside the plugin's
// own private directory, and only the HOST reads them:
//
//	llm-channels.json          the table: channels, their base URLs, model names, key ids
//	llm-key-<channel>-<key>.bin  one DPAPI-encrypted key per file (SecretKeeper)
//
// # THE THREE RULES
//
//  1. **A key is write-only.** `llm.channels.set` accepts `value`; nothing ever returns it.
//     Reads answer `{set, mask}` - the same promise ConfigStore makes about manifest
//     secrets, kept for runtime ones.
//  2. **The table never widens what a plugin may reach.** The base URL is checked against
//     THAT plugin's netHosts (plus its host-whitelist setting) at call time, because a
//     table row is otherwise a way to point the host's HTTP client anywhere.
//  3. **It is the plugin's own data, read on the plugin's own instruction.** `shareLLM` is
//     still the declaration that lets another plugin use these settings; without it the
//     table is private to its owner.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// channelFileName is the table's file inside a plugin's private directory.
const channelFileName = "llm-channels.json"

// The bounds. They are small on purpose: this is a person's model account list, not a
// gateway fleet, and every row costs the UI a form.
const (
	maxModelChannels  = 8
	maxChannelModels  = 16
	maxChannelKeys    = 8
	maxChannelBaseURL = 300
	maxChannelModel   = 96
	maxChannelKeyID   = 32
	maxChannelLabel   = 40
)

var channelIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// ModelChannelKey is one credential slot of a channel.
//
// `Value` is the write-only side: `llm.channels.set` may carry it, `llm.channels.get` never
// returns it.
type ModelChannelKey struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
	Value string `json:"value,omitempty"`

	// Set and Mask are filled by the host when the table is read.
	Set  bool   `json:"set,omitempty"`
	Mask string `json:"mask,omitempty"`
}

// ModelChannel is one endpoint plus the models and keys that belong to it.
type ModelChannel struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	BaseURL string `json:"baseUrl"`
	// Provider is the store's preset id ("openai", "ollama", ...). It is informational for
	// the UI and is what decides whether a key is REQUIRED (a local Ollama needs none).
	Provider     string            `json:"provider,omitempty"`
	Models       []string          `json:"models,omitempty"`
	DefaultModel string            `json:"defaultModel,omitempty"`
	Keys         []ModelChannelKey `json:"keys,omitempty"`
}

// ChannelTable is the whole file.
type ChannelTable struct {
	Version        int            `json:"version"`
	DefaultChannel string         `json:"defaultChannel,omitempty"`
	Channels       []ModelChannel `json:"channels"`
}

// ReadChannelTable loads a plugin's table. A missing or unreadable file is an EMPTY table
// rather than an error: "this plugin has no channels" is the honest reading of both, and the
// call path reports the real problem (no usable channel) with a better sentence.
func ReadChannelTable(privateDir string, keeper SecretKeeper) ChannelTable {
	table := ChannelTable{}
	raw, err := os.ReadFile(filepath.Join(privateDir, channelFileName))
	if err == nil {
		_ = json.Unmarshal(raw, &table)
	}
	for ci := range table.Channels {
		ch := &table.Channels[ci]
		for ki := range ch.Keys {
			key := &ch.Keys[ki]
			key.Value = ""
			if keeper == nil {
				continue
			}
			secret, err := keeper.Get(ChannelKeyPath(privateDir, ch.ID, key.ID))
			if err != nil || secret == "" {
				continue
			}
			key.Set = true
			key.Mask = MaskSecret(secret)
		}
	}
	return table
}

// WriteChannelTable validates and stores a table, and keeps the key files in step:
//
//   - a key that arrives WITH a value is encrypted into its own file;
//   - a key that arrives without one keeps whatever is already stored (so the UI can save
//     the list without holding the secrets);
//   - a key that is gone from the list has its file cleared (an orphaned credential is a
//     secret nobody can see or delete).
func WriteChannelTable(privateDir string, keeper SecretKeeper, table ChannelTable) (ChannelTable, error) {
	table.Version = 1
	if err := ValidateChannelTable(table); err != nil {
		return ChannelTable{}, err
	}
	if keeper == nil && hasAnyKeyValue(table) {
		return ChannelTable{}, errors.New("没有可用的加密存储，密钥不能保存")
	}

	previous := ReadChannelTable(privateDir, keeper)
	keep := make(map[string]bool, len(table.Channels)*2)
	for ci := range table.Channels {
		ch := &table.Channels[ci]
		for ki := range ch.Keys {
			key := &ch.Keys[ki]
			path := ChannelKeyPath(privateDir, ch.ID, key.ID)
			keep[path] = true
			if strings.TrimSpace(key.Value) != "" {
				if err := keeper.Set(path, strings.TrimSpace(key.Value)); err != nil {
					return ChannelTable{}, fmt.Errorf("保存密钥失败：%w", err)
				}
			}
			// The file is now the truth; the table only carries the id and the label.
			key.Value = ""
			secret, _ := keeper.Get(path)
			key.Set = secret != ""
			key.Mask = ""
			if key.Set {
				key.Mask = MaskSecret(secret)
			}
		}
	}
	for _, ch := range previous.Channels {
		for _, key := range ch.Keys {
			path := ChannelKeyPath(privateDir, ch.ID, key.ID)
			if keep[path] {
				continue
			}
			_ = keeper.Clear(path)
		}
	}

	if err := os.MkdirAll(privateDir, 0o755); err != nil {
		return ChannelTable{}, err
	}
	encoded, err := json.MarshalIndent(table, "", "  ")
	if err != nil {
		return ChannelTable{}, err
	}
	if err := os.WriteFile(filepath.Join(privateDir, channelFileName), encoded, 0o644); err != nil {
		return ChannelTable{}, err
	}
	return table, nil
}

// ChannelKeyPath is where one key lives. Both halves are pattern-checked by validation, so
// this can never become a path out of the directory.
func ChannelKeyPath(privateDir, channelID, keyID string) string {
	return filepath.Join(privateDir, "llm-key-"+channelID+"-"+keyID+".bin")
}

// ValidateChannelTable checks every rule the UI would otherwise have to remember.
func ValidateChannelTable(table ChannelTable) error {
	if len(table.Channels) > maxModelChannels {
		return fmt.Errorf("最多 %d 个渠道，收到 %d", maxModelChannels, len(table.Channels))
	}
	seenChannel := make(map[string]bool, len(table.Channels))
	for _, ch := range table.Channels {
		if !channelIDPattern.MatchString(strings.TrimSpace(ch.ID)) {
			return fmt.Errorf("渠道 id 非法：%q（字母、数字、下划线或连字符，≤32）", ch.ID)
		}
		if seenChannel[ch.ID] {
			return fmt.Errorf("渠道 id 重复：%s", ch.ID)
		}
		seenChannel[ch.ID] = true
		if len(ch.Name) > maxChannelLabel {
			return fmt.Errorf("渠道 %s 的名字太长（≤%d）", ch.ID, maxChannelLabel)
		}
		base := strings.TrimSpace(ch.BaseURL)
		if base == "" {
			return fmt.Errorf("渠道 %s 缺少接口地址", ch.ID)
		}
		if len(base) > maxChannelBaseURL {
			return fmt.Errorf("渠道 %s 的地址太长（≤%d）", ch.ID, maxChannelBaseURL)
		}
		lower := strings.ToLower(base)
		if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
			return fmt.Errorf("渠道 %s 的地址必须是 http(s)：%s", ch.ID, base)
		}
		if len(ch.Models) > maxChannelModels {
			return fmt.Errorf("渠道 %s 最多 %d 个模型", ch.ID, maxChannelModels)
		}
		for _, model := range ch.Models {
			if strings.TrimSpace(model) == "" || len(model) > maxChannelModel {
				return fmt.Errorf("渠道 %s 有空的或过长的模型名", ch.ID)
			}
		}
		if ch.DefaultModel != "" && len(ch.DefaultModel) > maxChannelModel {
			return fmt.Errorf("渠道 %s 的默认模型名太长", ch.ID)
		}
		if len(ch.Keys) > maxChannelKeys {
			return fmt.Errorf("渠道 %s 最多 %d 个 key", ch.ID, maxChannelKeys)
		}
		seenKey := make(map[string]bool, len(ch.Keys))
		for _, key := range ch.Keys {
			if !channelIDPattern.MatchString(strings.TrimSpace(key.ID)) {
				return fmt.Errorf("渠道 %s 的 key id 非法：%q", ch.ID, key.ID)
			}
			if seenKey[key.ID] {
				return fmt.Errorf("渠道 %s 的 key id 重复：%s", ch.ID, key.ID)
			}
			seenKey[key.ID] = true
			if len(key.Label) > maxChannelLabel {
				return fmt.Errorf("渠道 %s 的 key 标签太长（≤%d）", ch.ID, maxChannelLabel)
			}
			if len(key.Value) > 4096 {
				return fmt.Errorf("渠道 %s 的 key 太长", ch.ID)
			}
		}
	}
	if table.DefaultChannel != "" && !seenChannel[table.DefaultChannel] {
		return fmt.Errorf("默认渠道 %s 不在列表里", table.DefaultChannel)
	}
	return nil
}

// Keys are reported with MaskSecret (config.go), the same function manifest secrets use: one
// idea of "how much of a secret may be shown" for both.

func hasAnyKeyValue(table ChannelTable) bool {
	for _, ch := range table.Channels {
		for _, key := range ch.Keys {
			if strings.TrimSpace(key.Value) != "" {
				return true
			}
		}
	}
	return false
}

// ChannelsGet answers this plugin's own table (keys as {set, mask}).
//
// It lives here rather than in one program because BOTH hosts serve it: the channel
// plugin's editor runs in the control center's 插件 page, and the pet needs the same file
// when it makes the call. Two implementations of "where does the table live" is exactly the
// drift this package exists to prevent (the same reasoning as share.go).
func ChannelsGet(gate Gate, keeper SecretKeeper, id string) (any, error) {
	dir, err := gate.PrivateDir(id)
	if err != nil {
		return nil, err
	}
	table := ReadChannelTable(dir, keeper)
	return map[string]any{
		"channels":       table.Channels,
		"defaultChannel": table.DefaultChannel,
	}, nil
}

// ChannelsSet replaces this plugin's own table (see WriteChannelTable for the rules).
func ChannelsSet(gate Gate, keeper SecretKeeper, id string, payload []byte) (any, error) {
	var in struct {
		Channels       []ModelChannel `json:"channels"`
		DefaultChannel string         `json:"defaultChannel"`
	}
	if err := DecodePayload(payload, &in); err != nil {
		return nil, err
	}
	dir, err := gate.PrivateDir(id)
	if err != nil {
		return nil, err
	}
	table, err := WriteChannelTable(dir, keeper, ChannelTable{
		Channels:       in.Channels,
		DefaultChannel: in.DefaultChannel,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"channels":       table.Channels,
		"defaultChannel": table.DefaultChannel,
	}, nil
}

// RegisterChannels adds the two channel-table methods to a program's capability table.
//
// Gated on llm.chat, and that is the whole authorisation story: a table is THIS plugin's own
// model configuration - no other plugin's data is read, and no request is made. That is also
// why the control center can serve it: it needs no pet window, no budget and no credential of
// its own.
func RegisterChannels(r *Registry, gate Gate, keeper SecretKeeper) {
	r.Register("llm.channels.get", Method{
		Perm: PermLLMChat,
		Run: func(_ any, call Call, _ []byte) (any, error) {
			return ChannelsGet(gate, keeper, call.PluginID)
		},
	})
	r.Register("llm.channels.set", Method{
		Perm: PermLLMChat,
		Run: func(_ any, call Call, payload []byte) (any, error) {
			return ChannelsSet(gate, keeper, call.PluginID, payload)
		},
	})
}
