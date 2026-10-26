package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// --- fixture ---------------------------------------------------------------

// wsStub is a stand-in for the protocol side: a real WebSocket server that hands
// the accepted connection to the test.
//
// A real socket rather than a fake one on purpose. The load-bearing facts of the
// net.ws capability are "the Authorization header arrived", "an inbound frame
// became a ws.message", "a ws.send was seen on the wire" and "a dropped
// connection came back" - none of which a hand-written fake can testify to,
// because the fake would be asserting against itself.
type wsStub struct {
	srv *httptest.Server

	mu    sync.Mutex
	hits  int
	auths []string
	paths []string

	// accepted receives each upgraded connection, in order.
	accepted chan *websocket.Conn
}

func newWSStub(t *testing.T) *wsStub {
	t.Helper()
	st := &wsStub{accepted: make(chan *websocket.Conn, 8)}
	upgrader := websocket.Upgrader{
		// The host is not a browser and sends no Origin; a check here would be
		// theatre.
		CheckOrigin: func(*http.Request) bool { return true },
	}
	st.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		st.mu.Lock()
		st.hits++
		st.auths = append(st.auths, r.Header.Get("Authorization"))
		st.paths = append(st.paths, r.URL.RequestURI())
		st.mu.Unlock()
		// Handing the connection over and returning is correct: the socket is
		// hijacked, so the handler going away does not close it.
		st.accepted <- conn
	}))
	t.Cleanup(func() {
		st.srv.Close()
		st.mu.Lock()
		hits := st.hits
		st.mu.Unlock()
		_ = hits
	})
	return st
}

// host is the "host:port" form the manifest's netHosts has to declare.
func (s *wsStub) host() string { return strings.TrimPrefix(s.srv.URL, "http://") }

// wsURL is the ws:// address a plugin would be configured with.
func (s *wsStub) wsURL() string { return "ws" + strings.TrimPrefix(s.srv.URL, "http") + "/event" }

func (s *wsStub) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits
}

func (s *wsStub) lastAuth() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.auths) == 0 {
		return ""
	}
	return s.auths[len(s.auths)-1]
}

// accept waits for the next upgraded connection.
func (s *wsStub) accept(t *testing.T) *websocket.Conn {
	t.Helper()
	select {
	case c := <-s.accepted:
		return c
	case <-time.After(3 * time.Second):
		t.Fatal("no connection arrived at the stub server")
		return nil
	}
}

// wsTestManifest declares the socket capability and the host it may reach.
func wsTestManifest(hosts ...string) map[string]any {
	return map[string]any{
		"permissions": []string{"net.ws"},
		"netHosts":    hosts,
		"background":  true,
	}
}

// openWS asks the host to open a connection, exactly as a plugin would.
func openWS(t *testing.T, a *App, id, url, token string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"url": url, "token": token})
	if _, err := a.PluginRequest(id, "ws.open", string(payload)); err != nil {
		t.Fatalf("ws.open: %v", err)
	}
}

// wsStateOf reads the capability's own view of the connection.
func wsStateOf(t *testing.T, a *App, id string) pluginWSView {
	t.Helper()
	out, err := a.PluginRequest(id, "ws.state", "{}")
	if err != nil {
		t.Fatalf("ws.state: %v", err)
	}
	var view pluginWSView
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("ws.state returned something else: %v (%s)", err, out)
	}
	return view
}

// waitWSEvent waits until the collector has seen a ws.state with this state.
func waitWSEvent(t *testing.T, events func(string) []PluginEventView, want string) pluginWSView {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, ev := range events("ws.state") {
			view, ok := ev.Payload.(pluginWSView)
			if ok && view.State == want {
				return view
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no ws.state with state %q arrived; got %d frames", want, len(events("ws.state")))
	return pluginWSView{}
}

// --- refusal ---------------------------------------------------------------

// TestPluginWSOpenRefusesUnlistedHost pins the allow-list.
//
// The check happens before any dial, so a refused call must leave the plugin with
// no connection at all: a plugin cannot use a rejected address as a probe for what
// is listening on this machine.
func TestPluginWSOpenRefusesUnlistedHost(t *testing.T) {
	stub := newWSStub(t)
	a := pluginApp(t)
	// Declared, but a DIFFERENT port: the list is an exact match, so a stub on
	// another port must stay unreachable.
	id := installSampleWith(t, a, wsTestManifest("127.0.0.1:1"))

	payload, _ := json.Marshal(map[string]any{"url": stub.wsURL()})
	_, err := a.PluginRequest(id, "ws.open", string(payload))
	if err == nil {
		t.Fatal("an undeclared host must be refused")
	}
	if !strings.Contains(err.Error(), "未声明") {
		t.Fatalf("the refusal does not name the allow-list: %v", err)
	}
	if got := stub.count(); got != 0 {
		t.Fatalf("the stub was contacted %d times, want 0: the refusal must precede the dial", got)
	}
	if got := wsStateOf(t, a, id).State; got != pluginWSStateClosed {
		t.Fatalf("state after a refusal is %q, want %q", got, pluginWSStateClosed)
	}
}

// TestPluginWSOpenRefusesNonWebSocketScheme pins the scheme check: net.fetch
// already talks http, so a plugin that typed its http URL into the socket field
// must be told rather than silently connected to a web server.
func TestPluginWSOpenRefusesNonWebSocketScheme(t *testing.T) {
	stub := newWSStub(t)
	a := pluginApp(t)
	id := installSampleWith(t, a, wsTestManifest(stub.host()))

	payload, _ := json.Marshal(map[string]any{"url": stub.srv.URL + "/event"})
	_, err := a.PluginRequest(id, "ws.open", string(payload))
	if err == nil {
		t.Fatal("an http:// address must be refused")
	}
	if !strings.Contains(err.Error(), "ws/wss") {
		t.Fatalf("the refusal does not name the scheme: %v", err)
	}
}

// TestPluginWSRequiresThePermission pins the gate: the SAME host, declared and
// dialable, is refused while llm-style permissions are held instead.
func TestPluginWSRequiresThePermission(t *testing.T) {
	stub := newWSStub(t)
	a := pluginApp(t)
	id := installTestPlugin(t, a, wsTestManifest(stub.host()))
	// Installed and enabled, but net.ws not granted.
	managerSetEnabled(t, a, id, true)

	payload, _ := json.Marshal(map[string]any{"url": stub.wsURL()})
	_, err := a.PluginRequest(id, "ws.open", string(payload))
	if err == nil {
		t.Fatal("ws.open without the net.ws grant must be refused")
	}
	if !strings.Contains(err.Error(), "net.ws") {
		t.Fatalf("the refusal does not name the permission: %v", err)
	}
	if got := stub.count(); got != 0 {
		t.Fatalf("the stub was contacted %d times, want 0", got)
	}
}

// --- the working path ------------------------------------------------------

// TestPluginWSCarriesBothDirections pins the whole happy path: the connection is
// established against a real server, an inbound frame becomes a ws.message, and a
// ws.send reaches the other end.
func TestPluginWSCarriesBothDirections(t *testing.T) {
	stub := newWSStub(t)
	a := pluginApp(t)
	events := collectPluginEvents(a)
	id := installSampleWith(t, a, wsTestManifest(stub.host()))

	openWS(t, a, id, stub.wsURL(), "")
	server := stub.accept(t)
	defer server.Close()

	waitWSEvent(t, events, pluginWSStateOpen)
	if got := wsStateOf(t, a, id).State; got != pluginWSStateOpen {
		t.Fatalf("ws.state says %q, want open", got)
	}

	// Inbound: one text frame becomes exactly one ws.message carrying the text
	// verbatim, because the plugin (not the host) understands the protocol.
	const inbound = `{"event_type":"message_receive","self_id":10001,"data":{"message_scene":"group"}}`
	if err := server.WriteMessage(websocket.TextMessage, []byte(inbound)); err != nil {
		t.Fatalf("the stub could not write: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	var gotText string
	for time.Now().Before(deadline) && gotText == "" {
		for _, ev := range events("ws.message") {
			if m, ok := ev.Payload.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					gotText = s
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if gotText != inbound {
		t.Fatalf("ws.message carried %q, want %q", gotText, inbound)
	}

	// Outbound.
	out, _ := json.Marshal(map[string]any{"text": `{"action":"send_group_message"}`})
	if _, err := a.PluginRequest(id, "ws.send", string(out)); err != nil {
		t.Fatalf("ws.send: %v", err)
	}
	_ = server.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, data, err := server.ReadMessage()
	if err != nil {
		t.Fatalf("the stub did not receive the frame: %v", err)
	}
	if string(data) != `{"action":"send_group_message"}` {
		t.Fatalf("the stub received %q", data)
	}
	if view := wsStateOf(t, a, id); view.Received != 1 || view.Sent != 1 {
		t.Fatalf("counters are received=%d sent=%d, want 1/1", view.Received, view.Sent)
	}
}

// TestPluginWSSendsTheTokenAsAHeader is the reason the host owns the socket at
// all: a browser WebSocket cannot set a request header, so a token-protected
// protocol side is unreachable from the page.
func TestPluginWSSendsTheTokenAsAHeader(t *testing.T) {
	stub := newWSStub(t)
	a := pluginApp(t)
	events := collectPluginEvents(a)
	id := installSampleWith(t, a, wsTestManifest(stub.host()))

	openWS(t, a, id, stub.wsURL(), "s3cret-token")
	server := stub.accept(t)
	defer server.Close()
	waitWSEvent(t, events, pluginWSStateOpen)

	if got := stub.lastAuth(); got != "Bearer s3cret-token" {
		t.Fatalf("Authorization header was %q, want the bearer token", got)
	}
}

// TestPluginWSStateNeverCarriesTheToken pins the redaction.
//
// The reason ws.open takes a URL is that these protocols put their token in the
// query string, which means the string would otherwise reach a plugin-authored
// dashboard - and a screenshot of it.
func TestPluginWSStateNeverCarriesTheToken(t *testing.T) {
	stub := newWSStub(t)
	a := pluginApp(t)
	events := collectPluginEvents(a)
	id := installSampleWith(t, a, wsTestManifest(stub.host()))

	url := stub.wsURL() + "?access_token=super-secret"
	openWS(t, a, id, url, "")
	server := stub.accept(t)
	defer server.Close()
	waitWSEvent(t, events, pluginWSStateOpen)

	// The stub must still have received the real query - redaction is about what
	// we SHOW, not about what we send.
	stub.mu.Lock()
	paths := append([]string{}, stub.paths...)
	stub.mu.Unlock()
	if len(paths) == 0 || !strings.Contains(paths[0], "access_token=super-secret") {
		t.Fatalf("the dialled path was %v, want the token to reach the server", paths)
	}

	raw, err := json.Marshal(events("ws.state"))
	if err != nil {
		t.Fatalf("marshal events: %v", err)
	}
	if strings.Contains(string(raw), "super-secret") {
		t.Fatalf("a ws.state frame leaked the token: %s", raw)
	}
	if view := wsStateOf(t, a, id); strings.Contains(view.URL, "super-secret") {
		t.Fatalf("ws.state returned the token in its url: %q", view.URL)
	} else if !strings.Contains(view.URL, "access_token=***") {
		t.Fatalf("ws.state url is %q, want the query redacted rather than dropped", view.URL)
	}
}

// --- failure handling ------------------------------------------------------

// TestPluginWSReconnectsAfterTheServerDrops pins the supervisor: a dropped
// connection comes back on its own.
//
// Without this a bridge is dead after the first network blip, and the only
// symptom is a bot that stops answering - which is indistinguishable from a user
// who was not addressed.
func TestPluginWSReconnectsAfterTheServerDrops(t *testing.T) {
	stub := newWSStub(t)
	a := pluginApp(t)
	events := collectPluginEvents(a)
	id := installSampleWith(t, a, wsTestManifest(stub.host()))

	hub := a.pluginWS()
	hub.backoff = []time.Duration{10 * time.Millisecond}

	openWS(t, a, id, stub.wsURL(), "")
	first := stub.accept(t)
	waitWSEvent(t, events, pluginWSStateOpen)

	// The far end hangs up.
	_ = first.Close()

	second := stub.accept(t)
	defer second.Close()
	waitWSEvent(t, events, pluginWSStateOpen)

	if got := stub.count(); got < 2 {
		t.Fatalf("the stub accepted %d connections, want at least 2", got)
	}
	// It must be USABLE again, not merely re-dialled.
	if err := second.WriteMessage(websocket.TextMessage, []byte("back")); err != nil {
		t.Fatalf("the second connection is not writable: %v", err)
	}
}

// TestPluginWSCloseStopsRetrying pins the difference between "dropped" and
// "closed": after ws.close the supervisor must stop, or a plugin the user just
// switched off would keep reconnecting to their bot forever.
func TestPluginWSCloseStopsRetrying(t *testing.T) {
	a := pluginApp(t)
	events := collectPluginEvents(a)
	// A port nothing listens on: the first dial fails, which is the state the
	// supervisor would otherwise retry from for ever.
	id := installSampleWith(t, a, wsTestManifest("127.0.0.1:1"))

	hub := a.pluginWS()
	hub.backoff = []time.Duration{10 * time.Millisecond}

	openWS(t, a, id, "ws://127.0.0.1:1/event", "")
	waitWSEvent(t, events, pluginWSStateError)

	if _, err := a.PluginRequest(id, "ws.close", "{}"); err != nil {
		t.Fatalf("ws.close: %v", err)
	}
	before := len(events("ws.state"))
	time.Sleep(120 * time.Millisecond)
	after := len(events("ws.state"))
	if after != before {
		t.Fatalf("ws.state kept arriving after close (%d -> %d frames)", before, after)
	}
	if got := wsStateOf(t, a, id).State; got != pluginWSStateClosed {
		t.Fatalf("state after close is %q, want %q", got, pluginWSStateClosed)
	}
	// Sending after close is an error rather than a silent no-op: a plugin that
	// believed its reply went out would have no way to retry.
	if _, err := a.PluginRequest(id, "ws.send", `{"text":"x"}`); err == nil {
		t.Fatal("ws.send after close must be refused")
	}
}

// TestPluginWSClosesWhenThePluginIsDisabled pins the liveness tick.
//
// A plugin disabled while its socket is healthy has no way to tell us it is gone:
// the frame is destroyed with it. Without the tick the connection would keep
// receiving the group messages of a bot the user just switched off.
func TestPluginWSClosesWhenThePluginIsDisabled(t *testing.T) {
	stub := newWSStub(t)
	a := pluginApp(t)
	events := collectPluginEvents(a)
	id := installSampleWith(t, a, wsTestManifest(stub.host()))

	hub := a.pluginWS()
	hub.pingEvery = 20 * time.Millisecond

	openWS(t, a, id, stub.wsURL(), "")
	server := stub.accept(t)
	defer server.Close()
	waitWSEvent(t, events, pluginWSStateOpen)

	managerSetEnabled(t, a, id, false)

	// Read the hub directly rather than through ws.state: once the plugin is
	// disabled the capability gate refuses every call ("插件未启用"), which is
	// correct and is exactly why the socket must be closed by the HOST.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if hub.state(id).State == pluginWSStateClosed {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the connection survived the plugin being disabled")
}

// TestPluginWSSendIsRefusedBeforeConnect pins the ordering a plugin depends on:
// ws.send before ws.open is an error naming the missing step, not a panic and not
// a silent success.
func TestPluginWSSendIsRefusedBeforeConnect(t *testing.T) {
	stub := newWSStub(t)
	a := pluginApp(t)
	id := installSampleWith(t, a, wsTestManifest(stub.host()))

	_, err := a.PluginRequest(id, "ws.send", `{"text":"x"}`)
	if err == nil {
		t.Fatal("ws.send without a connection must be refused")
	}
	if !strings.Contains(err.Error(), "ws.open") {
		t.Fatalf("the refusal does not name the missing step: %v", err)
	}
}

// TestPluginWSCapabilityMethodsAreRegistered keeps the four verbs and the
// permission attached to them in one place: a rename in the table without a
// matching function (or the reverse) is a capability the plugin cannot call.
func TestPluginWSCapabilityMethodsAreRegistered(t *testing.T) {
	for _, name := range []string{"ws.open", "ws.send", "ws.close", "ws.state"} {
		spec, ok := pluginMethods[name]
		if !ok {
			t.Errorf("%s is not in the capability table", name)
			continue
		}
		if spec.perm != "net.ws" {
			t.Errorf("%s requires %q, want net.ws", name, spec.perm)
		}
	}
	if _, ok := pluginMethods["llm.profile"]; !ok {
		t.Error("llm.profile is not in the capability table: llm.borrow would then be a permission with no method")
	}
}
