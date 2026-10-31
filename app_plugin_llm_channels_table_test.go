package main

// 渠道表接入 `llm.chat`：多端点、多模型、多密钥，以及两道边界。
//
// openspec/changes/llm-channels。这里驱动的是**插件方法**（llm.channels.set / get）与真实请求路径，
// 而不是内部函数：一个渠道能不能用，最终的判据是"宿主有没有把请求发到那个地址、带着那个模型"。
//
// 两道边界各有两条用例：**没有密钥就不发请求**（不能拿别人的账号试），**地址不在 netHosts 里就拒绝**
// （否则表里的一行就是指向任意地址的 SSRF 通道）。

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

// channelHostOf returns "host:port" for a stub URL, which is the shape netHosts takes.
func channelHostOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		t.Fatalf("bad stub url %q: %v", raw, err)
	}
	return u.Host
}

// installChannelPlugin installs a plugin whose manifest may reach the given hosts.
func installChannelPlugin(t *testing.T, a *App, id, name string, hosts ...string) {
	t.Helper()
	man := chatTestManifest()
	man["id"] = id
	man["name"] = name
	man["netHosts"] = hosts
	installSampleWith(t, a, man)
}

// setChannels writes a table through the plugin method (the same path the UI uses).
func setChannels(t *testing.T, a *App, id string, channels []map[string]any, defaultChannel string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"channels": channels, "defaultChannel": defaultChannel})
	if err != nil {
		t.Fatalf("marshal channels: %v", err)
	}
	if _, err := a.PluginRequest(id, "llm.channels.set", string(payload)); err != nil {
		t.Fatalf("llm.channels.set: %v", err)
	}
}

// oneChannel builds a channel row pointing at the stub.
func oneChannel(id, base, model string, keys ...map[string]any) map[string]any {
	row := map[string]any{
		"id":           id,
		"name":         id,
		"baseUrl":      base,
		"provider":     "custom",
		"models":       []string{model},
		"defaultModel": model,
	}
	if len(keys) > 0 {
		row["keys"] = keys
	}
	return row
}

func key(id, value string) map[string]any {
	return map[string]any{"id": id, "label": id, "value": value}
}

// TestLLMChatUsesTheChannelTable is the feature: a plugin's own table answers, with the
// channel's default model, and the key it stored.
func TestLLMChatUsesTheChannelTable(t *testing.T) {
	stub := newChatStub(t)
	a := pluginApp(t)
	installChannelPlugin(t, a, borrowerID, "模型渠道", channelHostOf(t, stub.URL()))
	setChannels(t, a, borrowerID, []map[string]any{
		oneChannel("openai", stub.URL(), "gpt-4o-mini", key("main", "sk-test-1234567890")),
	}, "openai")

	if _, err := a.PluginRequest(borrowerID, "llm.chat", `{"text":"在吗"}`); err != nil {
		t.Fatalf("llm.chat with a channel table: %v", err)
	}
	if got := stub.Hits(); got != 1 {
		t.Fatalf("the channel received %d requests, want 1", got)
	}
	if got := stub.RequestModel(); got != "gpt-4o-mini" {
		t.Fatalf("the request carried model %q, want the channel's default", got)
	}

	// 读回来的表里，密钥只有 mask。
	out, err := a.PluginRequest(borrowerID, "llm.channels.get", "{}")
	if err != nil {
		t.Fatalf("llm.channels.get: %v", err)
	}
	if strings.Contains(out, "sk-test-1234567890") {
		t.Fatalf("llm.channels.get leaked the key: %s", out)
	}
	var view struct {
		Channels []struct {
			Keys []struct {
				Set  bool   `json:"set"`
				Mask string `json:"mask"`
			} `json:"keys"`
		} `json:"channels"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("channels.get returned something else: %v (%s)", err, out)
	}
	if len(view.Channels) != 1 || len(view.Channels[0].Keys) != 1 || !view.Channels[0].Keys[0].Set {
		t.Fatalf("the stored key is not reported as set: %s", out)
	}
	if view.Channels[0].Keys[0].Mask == "" {
		t.Fatal("the stored key has no mask")
	}
}

// TestLLMChatChannelSelectionAndModelOverride: the caller can pick the row and the model.
func TestLLMChatChannelSelectionAndModelOverride(t *testing.T) {
	first := newChatStub(t)
	second := newChatStub(t)
	a := pluginApp(t)
	installChannelPlugin(t, a, borrowerID, "模型渠道",
		channelHostOf(t, first.URL()), channelHostOf(t, second.URL()))
	setChannels(t, a, borrowerID, []map[string]any{
		oneChannel("a-first", first.URL(), "first-model", key("k", "sk-first-1234567890")),
		oneChannel("b-second", second.URL(), "second-model", key("k", "sk-second-1234567890")),
	}, "a-first")

	// 默认走 defaultChannel
	if _, err := a.PluginRequest(borrowerID, "llm.chat", `{"text":"一"}`); err != nil {
		t.Fatalf("default channel: %v", err)
	}
	if first.Hits() != 1 || second.Hits() != 0 {
		t.Fatalf("default channel not used: first=%d second=%d", first.Hits(), second.Hits())
	}

	// 点名第二个渠道 + 覆盖模型
	payload := `{"text":"二","channel":"b-second","model":"override-model"}`
	if _, err := a.PluginRequest(borrowerID, "llm.chat", payload); err != nil {
		t.Fatalf("named channel: %v", err)
	}
	if second.Hits() != 1 {
		t.Fatalf("the named channel received %d requests", second.Hits())
	}
	if got := second.RequestModel(); got != "override-model" {
		t.Fatalf("per-call model override was ignored: %q", got)
	}
}

// TestLLMChatChannelWithoutAKeyIsRefused: no credential, no request. Trying with an empty
// Authorization would burn a round trip and produce an auth error that reads like a bug in
// the host rather than a missing key.
func TestLLMChatChannelWithoutAKeyIsRefused(t *testing.T) {
	stub := newChatStub(t)
	a := pluginApp(t)
	installChannelPlugin(t, a, borrowerID, "模型渠道", channelHostOf(t, stub.URL()))
	// 一个只有 id 的 key：表里存在，但没有密钥。
	setChannels(t, a, borrowerID, []map[string]any{
		oneChannel("openai", stub.URL(), "gpt-4o", map[string]any{"id": "empty", "label": "还没填"}),
	}, "openai")

	_, err := a.PluginRequest(borrowerID, "llm.chat", `{"text":"在吗"}`)
	if err == nil {
		t.Fatal("a channel without a stored key must not send a request")
	}
	if !strings.Contains(err.Error(), "渠道") {
		t.Fatalf("the refusal should point at the channel: %v", err)
	}
	if stub.Hits() != 0 {
		t.Fatalf("the stub received %d requests without a key", stub.Hits())
	}
}

// TestLLMChatChannelHostMustBeAllowed: a table row is plugin-written data, so the address
// has to pass the SAME allow-list net.fetch uses - otherwise a row is an SSRF primitive.
func TestLLMChatChannelHostMustBeAllowed(t *testing.T) {
	stub := newChatStub(t)
	a := pluginApp(t)
	// 声明一个不相关的 host：stub 的地址因此不在白名单里。
	installChannelPlugin(t, a, borrowerID, "模型渠道", "api.example.com")
	setChannels(t, a, borrowerID, []map[string]any{
		oneChannel("sneaky", stub.URL(), "gpt-4o", key("k", "sk-test-1234567890")),
	}, "sneaky")

	_, err := a.PluginRequest(borrowerID, "llm.chat", `{"text":"在吗"}`)
	if err == nil {
		t.Fatal("a channel pointing outside netHosts must be refused")
	}
	if !strings.Contains(err.Error(), "netHosts") {
		t.Fatalf("the refusal must name the allow-list: %v", err)
	}
	if stub.Hits() != 0 {
		t.Fatalf("the host reached %d times past its allow-list", stub.Hits())
	}
}

// TestLLMChatBorrowsAChannelOnlyLender: a lender that has NO manifest endpoint but a table
// is still a lender - that is the shape the channel plugin has.
func TestLLMChatBorrowsAChannelOnlyLender(t *testing.T) {
	stub := newChatStub(t)
	a := pluginApp(t)
	man := chatTestManifest()
	man["id"] = lenderID
	man["name"] = "模型渠道（出借）"
	man["shareLLM"] = true
	man["netHosts"] = []string{channelHostOf(t, stub.URL())}
	installSampleWith(t, a, man)
	// 注意：不写 llmBaseUrl / llmModel —— 只有渠道表。
	setChannels(t, a, lenderID, []map[string]any{
		oneChannel("openai", stub.URL(), "gpt-4o-mini", key("main", "sk-lender-1234567890")),
	}, "openai")
	installBorrower(t, a, true)

	if _, err := a.PluginRequest(borrowerID, "llm.chat", `{"text":"借一下"}`); err != nil {
		t.Fatalf("borrowing a channel-only lender: %v", err)
	}
	if stub.Hits() != 1 {
		t.Fatalf("the lender's channel received %d requests, want 1", stub.Hits())
	}

	// llm.providers 也该把它算成 ready。
	out, err := a.PluginRequest(borrowerID, "llm.providers", "{}")
	if err != nil {
		t.Fatalf("llm.providers: %v", err)
	}
	var view struct {
		Providers []struct {
			ID    string `json:"id"`
			Ready bool   `json:"ready"`
		} `json:"providers"`
		Default string `json:"default"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("llm.providers returned something else: %v (%s)", err, out)
	}
	if view.Default != lenderID {
		t.Fatalf("default = %q, want the channel-only lender %q", view.Default, lenderID)
	}
	for _, p := range view.Providers {
		if p.ID == lenderID && !p.Ready {
			t.Fatalf("the channel-only lender is listed as not ready: %s", out)
		}
	}
}

// TestLLMChannelsSetNeedsChatPermission: writing a table is "my own model configuration",
// so it rides on llm.chat - and without it nothing is written.
func TestLLMChannelsSetNeedsChatPermission(t *testing.T) {
	a := pluginApp(t)
	man := chatTestManifest()
	man["id"] = borrowerID
	man["permissions"] = []string{"plugin.config", "storage"}
	installSampleWith(t, a, man)

	payload := `{"channels":[{"id":"x","baseUrl":"https://api.example.com/v1"}],"defaultChannel":"x"}`
	if _, err := a.PluginRequest(borrowerID, "llm.channels.set", payload); err == nil {
		t.Fatal("llm.channels.set must require llm.chat")
	}
}
