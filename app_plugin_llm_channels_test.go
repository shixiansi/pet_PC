package main

// Model channels: naming one, and listing them.
//
// openspec/changes/llm-channels. The two halves under test are small but they answer a
// question the old code could not: "which of my two endpoints does this group use".
//
//   · `llm.chat {provider: "<plugin id>"}` picks that channel, and ONLY that channel -
//     it must not fall back to the lowest-id lender when the named one is not ready,
//     because a silent fallback would spend the wrong account and look like it worked.
//   · `llm.providers` lists what exists, so a plugin can offer a choice (or say why the
//     name it was given is wrong) without the host having to invent a config UI per
//     provider.
//
// Both are inside the llm.borrow consent, and neither hands the endpoint or the key to the
// caller: those stay in the host, which is the whole point of lending rather than copying.

import (
	"encoding/json"
	"strings"
	"testing"
)

// channelID is one named channel in these tests. Sorts AFTER lenderID on purpose: the
// default rule is "lowest id wins", so a test that names this one proves the name beat the
// rule rather than accidentally matching it.
const channelID = "com.example.ollama"

// installChannel installs one more lender with its own endpoint and model.
func installChannel(t *testing.T, a *App, id, name, endpoint, model string) string {
	t.Helper()
	man := chatTestManifest()
	man["id"] = id
	man["name"] = name
	man["shareLLM"] = true
	installSampleWith(t, a, man)
	setPluginConfig(t, a, id, map[string]string{
		"llmBaseUrl": endpoint,
		"llmModel":   model,
	})
	return id
}

// TestLLMChatNamesAChannel is the feature: the caller says which endpoint to use.
func TestLLMChatNamesAChannel(t *testing.T) {
	defaultStub := newChatStub(t)
	namedStub := newChatStub(t)
	a := pluginApp(t)
	installLender(t, a, defaultStub.URL()) // com.example.lender, the default
	installChannel(t, a, channelID, "本地 Ollama", namedStub.URL(), "qwen2.5:7b")
	installBorrower(t, a, true)

	payload := `{"text":"群里问一句","provider":"` + channelID + `"}`
	if _, err := a.PluginRequest(borrowerID, "llm.chat", payload); err != nil {
		t.Fatalf("llm.chat with provider: %v", err)
	}

	if got := namedStub.Hits(); got != 1 {
		t.Fatalf("the named channel received %d requests, want 1", got)
	}
	if got := defaultStub.Hits(); got != 0 {
		t.Fatalf("the default channel received %d requests: naming a channel must not fall back", got)
	}
	if got := namedStub.RequestModel(); got != "qwen2.5:7b" {
		t.Fatalf("the request carried model %q, want the named channel's", got)
	}
}

// TestLLMChatNamedChannelMustBeReady: a name that cannot answer fails LOUDLY, and does not
// quietly become the default. "It worked, but on the other account" is the failure this pins.
func TestLLMChatNamedChannelMustBeReady(t *testing.T) {
	defaultStub := newChatStub(t)
	a := pluginApp(t)
	installLender(t, a, defaultStub.URL())
	// Declares shareLLM but has no endpoint/model: it exists, it is just not usable.
	man := chatTestManifest()
	man["id"] = channelID
	man["name"] = "空的渠道"
	man["shareLLM"] = true
	installSampleWith(t, a, man)
	installBorrower(t, a, true)

	_, err := a.PluginRequest(borrowerID, "llm.chat", `{"text":"在吗","provider":"`+channelID+`"}`)
	if err == nil {
		t.Fatal("a channel that is not configured must not answer")
	}
	if !strings.Contains(err.Error(), channelID) {
		t.Fatalf("the refusal must name the channel: %v", err)
	}
	if defaultStub.Hits() != 0 {
		t.Fatal("a failed named channel fell back to the default lender")
	}
}

// TestLLMChatProviderUnknownAndSelf keeps the two easy mistakes readable: a typo, and
// naming yourself.
func TestLLMChatProviderUnknownAndSelf(t *testing.T) {
	a := pluginApp(t)
	installLender(t, a, newChatStub(t).URL())
	installBorrower(t, a, true)

	_, err := a.PluginRequest(borrowerID, "llm.chat", `{"text":"在吗","provider":"com.example.typo"}`)
	if err == nil || !strings.Contains(err.Error(), "com.example.typo") {
		t.Fatalf("an unknown channel must be refused by name: %v", err)
	}

	_, err = a.PluginRequest(borrowerID, "llm.chat", `{"text":"在吗","provider":"`+borrowerID+`"}`)
	if err == nil || !strings.Contains(err.Error(), "自己") {
		t.Fatalf("naming yourself must be refused: %v", err)
	}
}

// TestLLMChatProviderNeedsBorrowPermission: consent is still the outer gate. A plugin
// without llm.borrow cannot reach another plugin's endpoint by naming it.
func TestLLMChatProviderNeedsBorrowPermission(t *testing.T) {
	channelStub := newChatStub(t)
	a := pluginApp(t)
	installChannel(t, a, channelID, "本地 Ollama", channelStub.URL(), "qwen2.5:7b")
	installBorrower(t, a, false)

	_, err := a.PluginRequest(borrowerID, "llm.chat", `{"text":"在吗","provider":"`+channelID+`"}`)
	if err == nil {
		t.Fatal("naming a channel must still require llm.borrow")
	}
	if !strings.Contains(err.Error(), "llm.borrow") {
		t.Fatalf("the refusal must name the permission: %v", err)
	}
	if channelStub.Hits() != 0 {
		t.Fatal("the channel was called without the borrow permission")
	}
}

// TestLLMProvidersListsChannels is the listing half: what exists, which one is the default,
// and (the boundary) nothing about where they point or what unlocks them.
func TestLLMProvidersListsChannels(t *testing.T) {
	defaultStub := newChatStub(t)
	channelStub := newChatStub(t)
	a := pluginApp(t)
	installLender(t, a, defaultStub.URL())
	installChannel(t, a, channelID, "本地 Ollama", channelStub.URL(), "qwen2.5:7b")
	installBorrower(t, a, true)

	out, err := a.PluginRequest(borrowerID, "llm.providers", "{}")
	if err != nil {
		t.Fatalf("llm.providers: %v", err)
	}
	var view struct {
		Providers []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Model string `json:"model"`
			Ready bool   `json:"ready"`
			Self  bool   `json:"self"`
		} `json:"providers"`
		Default string `json:"default"`
		Own     struct {
			Ready bool   `json:"ready"`
			Model string `json:"model"`
		} `json:"own"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("llm.providers returned something else: %v (%s)", err, out)
	}
	if len(view.Providers) != 2 {
		t.Fatalf("providers = %d, want 2: %s", len(view.Providers), out)
	}
	// Sorted by id, and the ids are the routes a caller names.
	if view.Providers[0].ID != lenderID || view.Providers[1].ID != channelID {
		t.Fatalf("providers are not sorted by id: %s", out)
	}
	if view.Providers[1].Model != "qwen2.5:7b" || !view.Providers[1].Ready {
		t.Fatalf("the named channel's row is wrong: %s", out)
	}
	if view.Default != lenderID {
		t.Fatalf("default = %q, want the lowest ready lender (%q)", view.Default, lenderID)
	}
	if view.Own.Ready {
		t.Fatal("the borrower has no settings of its own; own.ready must be false")
	}
	// The boundary: never the endpoint, never the key.
	if strings.Contains(out, defaultStub.URL()) || strings.Contains(out, channelStub.URL()) {
		t.Fatalf("llm.providers leaked an endpoint: %s", out)
	}
	if strings.Contains(out, "sk-") {
		t.Fatalf("llm.providers leaked a credential: %s", out)
	}
}

// TestLLMProvidersNeedsBorrowPermission: the list itself is information about other
// plugins' configuration, so it lives behind the same permission.
func TestLLMProvidersNeedsBorrowPermission(t *testing.T) {
	a := pluginApp(t)
	installLender(t, a, newChatStub(t).URL())
	installBorrower(t, a, false)

	if _, err := a.PluginRequest(borrowerID, "llm.providers", "{}"); err == nil {
		t.Fatal("listing channels requires llm.borrow")
	}
}
