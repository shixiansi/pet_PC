package plugins

// 渠道表与密钥库：读写、校验、以及"密钥只回 mask"这条边界。
//
// 这一组用例守的是三件事：
//
//   1. **校验在写入之前**。渠道 id、地址、模型名、key id 都有上限与形状要求，因为 key 的
//      两半最终会拼成文件名（`llm-key-<渠道>-<key>.bin`），一个 `..` 就是一条路径。
//   2. **密钥是只写的**。写入之后，读回来只有 `{set, mask}`，而磁盘上的 JSON 里不该出现明文
//      ——这是 ConfigStore 对 manifest secret 的同一个承诺，运行时密钥也照办。
//   3. **列表就是真相**。保存时少了一个 key，它的密钥文件要跟着被清掉：否则那是一个谁都看不见、
//      也删不掉的凭据。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 复用 config_test.go 里的 newFakeKeeper：它同时记录调用次数，而这里要断言的是
// "什么时候读、什么时候清"，不是加密本身。

func oneChannelTable() ChannelTable {
	return ChannelTable{
		DefaultChannel: "openai",
		Channels: []ModelChannel{
			{
				ID:           "openai",
				Name:         "OpenAI",
				BaseURL:      "https://api.openai.com/v1",
				Provider:     "openai",
				Models:       []string{"gpt-4o", "gpt-4o-mini"},
				DefaultModel: "gpt-4o",
				Keys: []ModelChannelKey{
					{ID: "main", Label: "主号", Value: "sk-main-1234567890"},
					{ID: "spare", Label: "备用", Value: "sk-spare-0987654321"},
				},
			},
		},
	}
}

func TestChannelTableRoundTripKeepsSecretsWriteOnly(t *testing.T) {
	dir := t.TempDir()
	keeper := newFakeKeeper()

	stored, err := WriteChannelTable(dir, keeper, oneChannelTable())
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(stored.Channels) != 1 || len(stored.Channels[0].Keys) != 2 {
		t.Fatalf("stored table lost rows: %+v", stored)
	}
	for _, key := range stored.Channels[0].Keys {
		if key.Value != "" {
			t.Errorf("key %s came back with its plaintext", key.ID)
		}
		if !key.Set {
			t.Errorf("key %s was not marked as stored", key.ID)
		}
		if key.Mask == "" {
			t.Errorf("key %s has no mask, so the UI cannot tell keys apart", key.ID)
		}
	}

	// 磁盘上的 JSON 不能有明文。
	raw, err := os.ReadFile(filepath.Join(dir, channelFileName))
	if err != nil {
		t.Fatalf("read the table: %v", err)
	}
	if strings.Contains(string(raw), "sk-main") || strings.Contains(string(raw), "sk-spare") {
		t.Fatalf("the table file contains a plaintext key:\n%s", raw)
	}

	// 重新读一次，掩码与 set 都还在（它们是从密钥库算出来的，不是存下来的）。
	again := ReadChannelTable(dir, keeper)
	if len(again.Channels) != 1 || len(again.Channels[0].Keys) != 2 {
		t.Fatalf("reread lost rows: %+v", again)
	}
	if !again.Channels[0].Keys[0].Set || again.Channels[0].Keys[0].Mask == "" {
		t.Fatalf("reread lost the key state: %+v", again.Channels[0].Keys[0])
	}
}

func TestChannelTableSavesWithoutHoldingTheSecrets(t *testing.T) {
	dir := t.TempDir()
	keeper := newFakeKeeper()
	if _, err := WriteChannelTable(dir, keeper, oneChannelTable()); err != nil {
		t.Fatalf("first write: %v", err)
	}

	// 第二次保存时，key 不带 value —— 界面就是这么保存的：它从来没有密钥的明文。
	table := oneChannelTable()
	for i := range table.Channels[0].Keys {
		table.Channels[0].Keys[i].Value = ""
	}
	stored, err := WriteChannelTable(dir, keeper, table)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	for _, key := range stored.Channels[0].Keys {
		if !key.Set {
			t.Fatalf("saving without a value dropped key %s", key.ID)
		}
	}
	if len(keeper.values) != 2 {
		t.Fatalf("keeper holds %d secrets, want 2", len(keeper.values))
	}
}

func TestChannelTableRemovingAKeyClearsItsSecret(t *testing.T) {
	dir := t.TempDir()
	keeper := newFakeKeeper()
	if _, err := WriteChannelTable(dir, keeper, oneChannelTable()); err != nil {
		t.Fatalf("first write: %v", err)
	}

	table := oneChannelTable()
	table.Channels[0].Keys = table.Channels[0].Keys[:1] // 只留 main
	if _, err := WriteChannelTable(dir, keeper, table); err != nil {
		t.Fatalf("second write: %v", err)
	}
	if len(keeper.values) != 1 {
		t.Fatalf("keeper holds %d secrets, want 1 (the removed key must be cleared)", len(keeper.values))
	}
	if _, ok := keeper.values[ChannelKeyPath(dir, "openai", "spare")]; ok {
		t.Fatal("the removed key's file is still there")
	}
}

func TestChannelTableRefusesBadTables(t *testing.T) {
	base := func() ChannelTable {
		t := oneChannelTable()
		return t
	}
	cases := []struct {
		name   string
		mutate func(*ChannelTable)
		want   string
	}{
		{"no base url", func(t *ChannelTable) { t.Channels[0].BaseURL = "" }, "缺少接口地址"},
		{"not http", func(t *ChannelTable) { t.Channels[0].BaseURL = "ftp://x/y" }, "http(s)"},
		{"bad id", func(t *ChannelTable) { t.Channels[0].ID = "../etc" }, "渠道 id 非法"},
		{"duplicate channel", func(t *ChannelTable) {
			clone := t.Channels[0]
			t.Channels = append(t.Channels, clone)
		}, "重复"},
		{"duplicate key", func(t *ChannelTable) {
			t.Channels[0].Keys[1].ID = t.Channels[0].Keys[0].ID
		}, "key id 重复"},
		{"bad key id", func(t *ChannelTable) { t.Channels[0].Keys[0].ID = "a/b" }, "key id 非法"},
		{"default missing", func(t *ChannelTable) { t.DefaultChannel = "nope" }, "默认渠道"},
		{"too many channels", func(t *ChannelTable) {
			t.Channels = nil
			for i := 0; i < maxModelChannels+1; i++ {
				ch := oneChannelTable().Channels[0]
				ch.ID = string(rune('a'+i)) + "ch"
				ch.Keys = nil
				t.Channels = append(t.Channels, ch)
			}
		}, "最多"},
		{"too many keys", func(t *ChannelTable) {
			t.Channels[0].Keys = nil
			for i := 0; i < maxChannelKeys+1; i++ {
				t.Channels[0].Keys = append(t.Channels[0].Keys, ModelChannelKey{ID: "k" + string(rune('a'+i))})
			}
		}, "最多"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			table := base()
			tc.mutate(&table)
			err := ValidateChannelTable(table)
			if err == nil {
				t.Fatalf("%s: a bad table was accepted", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%s: error %q does not mention %q", tc.name, err, tc.want)
			}
		})
	}
}

func TestChannelTableWithoutKeeperRefusesSecrets(t *testing.T) {
	// 没有加密存储时不能"悄悄存明文"——这是 config.go 对 manifest secret 的同一条规矩。
	if _, err := WriteChannelTable(t.TempDir(), nil, oneChannelTable()); err == nil {
		t.Fatal("writing keys without a SecretKeeper must be refused")
	}
}

func TestReadChannelTableOfAnAbsentFileIsEmpty(t *testing.T) {
	table := ReadChannelTable(t.TempDir(), newFakeKeeper())
	if len(table.Channels) != 0 {
		t.Fatalf("an absent table read as %d channels", len(table.Channels))
	}
}
