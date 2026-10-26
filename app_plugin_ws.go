package main

// The net.ws capability: one managed WebSocket per plugin.
//
// WHY THE HOST OWNS THE SOCKET
//
// The plugin that motivated this (a chat-protocol bridge: Milky/Yogurt, OneBot,
// NapCat) receives its events over a connection it holds open, so net.fetch -
// one request, one response - has nothing to receive with. The obvious
// alternative is a WebSocket opened by the plugin's own page, and it does not
// work for three reasons that are all facts about this host rather than taste:
//
//  1. An access token has to travel as an HTTP header (`Authorization: Bearer`).
//     A browser `new WebSocket()` cannot set request headers at all, so every
//     token-protected protocol side would be unreachable from the page.
//  2. The page is disposable: a plugin update remounts its frames, and the
//     background frame is destroyed and rebuilt when the plugin list is
//     re-rendered. A socket in that page dies with it, which for a bridge means
//     "a message that arrived during the remount is lost".
//  3. net.fetch's whole design is "the host decides which hosts are reachable".
//     A page that could open its own socket would make that allow-list
//     decorative - the sandbox has no CSP, so nothing but our own discipline
//     stops it today.
//
// So the connection lives here. The plugin gets three verbs and one event
// stream, and never owns a socket handle:
//
//	ws.open  {url, token?}   validates the URL against the manifest's hosts and
//	                         starts a supervisor (returns immediately)
//	ws.send  {text}          one text frame, only while connected
//	ws.close {}              stops it; the supervisor stops retrying
//	ws.state {}              current state, for a plugin that just mounted
//
//	event ws.message {text}  one inbound text frame
//	event ws.state   {state, url, error, since, received, sent}
//
// WHICH HOSTS
//
// Exactly the net.fetch allow-list: the manifest's netHosts plus the value of the
// plugin's host-whitelist setting (plugins.ConfigHosts). One list for one
// question - "where may this plugin talk to" - so a plugin cannot reach a host
// through the socket that its requests are refused for.

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"PetPC/internal/plugins"
)

const (
	// pluginWSPingInterval keeps the connection alive through anything that
	// reaps idle sockets. It is also the liveness tick: a plugin that is
	// disabled while connected is closed on the next tick rather than on the
	// next frame, so "off" means off even for a silent service.
	pluginWSPingInterval = 30 * time.Second
	// pluginWSReadLimit is the largest inbound frame accepted. Milky event
	// payloads are small JSON objects; 8 MiB is generous for a long message with
	// forwarded segments and still bounds what one peer can make us buffer.
	pluginWSReadLimit = 8 << 20
	// pluginWSWriteTimeout bounds one WriteMessage.
	pluginWSWriteTimeout = 10 * time.Second
	// pluginWSMaxText caps one outbound frame. A plugin sending more than this is
	// either broken or building a message the protocol side will refuse anyway.
	pluginWSMaxText = 512 << 10
	// pluginWSMaxToken caps the access token a plugin may hand over.
	pluginWSMaxToken = 2048
)

// The three states a connection can be in, as the plugin sees them. "error" is a
// state rather than a terminal outcome: the supervisor keeps retrying, so the
// plugin's UI can show the last failure without treating it as final.
const (
	pluginWSStateConnecting = "connecting"
	pluginWSStateOpen       = "open"
	pluginWSStateError      = "error"
	pluginWSStateClosed     = "closed"
)

// wsSocket is the part of *websocket.Conn this file uses.
//
// It is an interface so the supervisor can be driven by a fake during a test:
// reconnect/backoff logic is the part most likely to be wrong, and it must be
// exercisable without a network.
type wsSocket interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(messageType int, data []byte) error
	WriteControl(messageType int, data []byte, deadline time.Time) error
	SetReadLimit(limit int64)
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	SetPongHandler(h func(string) error)
	Close() error
}

// dialWebSocket is the production dialer.
func dialWebSocket(rawURL string, header http.Header) (wsSocket, error) {
	dialer := &websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		// No proxy: this is a local/LAN service, and honouring a system proxy
		// here would send a loopback connection through it.
		Proxy: nil,
	}
	conn, resp, err := dialer.Dial(rawURL, header)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("%w（HTTP %s）", err, resp.Status)
		}
		return nil, err
	}
	return conn, nil
}

// pluginWSConn is one plugin's managed connection plus what the UI is told
// about it.
type pluginWSConn struct {
	pluginID string
	// rawURL is the exact address dialled, query string included. It may carry
	// an access token, so it is NEVER logged or emitted - display is what views
	// get.
	rawURL  string
	display string
	header  http.Header

	stop     chan struct{}
	stopOnce sync.Once

	// writeMu serialises writers. gorilla allows one concurrent writer, and a
	// plugin can call ws.send while the ping loop wants to send a control frame.
	writeMu sync.Mutex

	mu       sync.Mutex
	state    string
	lastErr  string
	since    time.Time
	received int64
	sent     int64
	socket   wsSocket
}

// snapshot is one consistent read of the connection's state.
type pluginWSView struct {
	State    string `json:"state"`
	URL      string `json:"url,omitempty"`
	Error    string `json:"error,omitempty"`
	Since    string `json:"since,omitempty"`
	Received int64  `json:"received"`
	Sent     int64  `json:"sent"`
}

func (c *pluginWSConn) view() pluginWSView {
	c.mu.Lock()
	defer c.mu.Unlock()
	return pluginWSView{
		State:    c.state,
		URL:      c.display,
		Error:    c.lastErr,
		Since:    c.since.Format(time.RFC3339),
		Received: c.received,
		Sent:     c.sent,
	}
}

func (c *pluginWSConn) setState(state, lastErr string) {
	c.mu.Lock()
	c.state = state
	if lastErr != "" {
		c.lastErr = lastErr
	}
	c.since = time.Now()
	c.mu.Unlock()
}

func (c *pluginWSConn) currentState() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *pluginWSConn) setSocket(s wsSocket) {
	c.mu.Lock()
	c.socket = s
	c.mu.Unlock()
}

func (c *pluginWSConn) currentSocket() wsSocket {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.socket
}

func (c *pluginWSConn) noteReceived() {
	c.mu.Lock()
	c.received++
	c.mu.Unlock()
}

func (c *pluginWSConn) noteSent() {
	c.mu.Lock()
	c.sent++
	c.mu.Unlock()
}

func (c *pluginWSConn) stopped() bool {
	select {
	case <-c.stop:
		return true
	default:
		return false
	}
}

// shutdown stops the supervisor and closes the socket. It is idempotent.
func (c *pluginWSConn) shutdown() {
	c.stopOnce.Do(func() {
		close(c.stop)
		if sock := c.currentSocket(); sock != nil {
			_ = sock.Close()
		}
		c.setState(pluginWSStateClosed, "")
	})
}

// pluginWSHub owns every plugin's connection.
//
// One connection per plugin id, not a pool: a plugin that wants two services can
// open, close, and open again, and "which socket does ws.send write to" stays a
// question with exactly one answer.
type pluginWSHub struct {
	mu    sync.Mutex
	conns map[string]*pluginWSConn

	// dial is the seam a test replaces.
	dial func(rawURL string, header http.Header) (wsSocket, error)
	// emit publishes one host->plugin event. It is a function rather than a
	// *App so the hub can be driven with a collector in a test.
	emit func(pluginID, name string, payload any)
	// alive reports whether a plugin is still installed and enabled. The
	// supervisor asks before every dial and on every ping tick, because a plugin
	// disabled while connected has no way to tell us it is gone.
	alive func(pluginID string) bool
	// wait sleeps for d unless the connection is stopped or the plugin goes
	// away, in which case it reports false. A test substitutes an instant one.
	wait func(c *pluginWSConn, d time.Duration) bool
	// backoff is the retry ladder, walked on consecutive failures and reset by a
	// successful connection.
	backoff []time.Duration
	// pingEvery is the keep-alive / liveness tick. It is a field rather than the
	// constant so a test can make "notice the plugin was disabled" happen in
	// milliseconds instead of half a minute.
	pingEvery time.Duration
}

// newPluginWSHub builds a hub with the production seams.
func newPluginWSHub(emit func(pluginID, name string, payload any), alive func(string) bool) *pluginWSHub {
	h := &pluginWSHub{
		conns: map[string]*pluginWSConn{},
		dial:  dialWebSocket,
		emit:  emit,
		alive: alive,
		backoff: []time.Duration{
			1 * time.Second, 2 * time.Second, 4 * time.Second,
			8 * time.Second, 15 * time.Second, 30 * time.Second,
		},
		pingEvery: pluginWSPingInterval,
	}
	h.wait = func(c *pluginWSConn, d time.Duration) bool { return sleepUnlessStopped(h, c, d) }
	return h
}

// sleepUnlessStopped waits in short slices so a close or a disable is noticed
// promptly instead of after the whole backoff step.
func sleepUnlessStopped(h *pluginWSHub, c *pluginWSConn, d time.Duration) bool {
	const slice = 250 * time.Millisecond
	for waited := time.Duration(0); waited < d; waited += slice {
		select {
		case <-c.stop:
			return false
		case <-time.After(slice):
		}
		if h.alive != nil && !h.alive(c.pluginID) {
			return false
		}
	}
	return !c.stopped()
}

func (h *pluginWSHub) backoffFor(attempt int) time.Duration {
	if len(h.backoff) == 0 {
		return time.Second
	}
	if attempt >= len(h.backoff) {
		return h.backoff[len(h.backoff)-1]
	}
	return h.backoff[attempt]
}

// emitState publishes the connection's current state.
func (h *pluginWSHub) emitState(c *pluginWSConn) {
	if h.emit == nil {
		return
	}
	h.emit(c.pluginID, "ws.state", c.view())
}

// open validates the address and starts the supervisor.
//
// It returns before the handshake, so the plugin's request does not block on a
// network round trip: everything after validation - connecting, failing,
// retrying, succeeding - arrives as ws.state events, which is also the only
// story that is true when the service is down at the moment the plugin starts.
func (h *pluginWSHub) open(pluginID, rawURL, token string, hosts []string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return errors.New("ws.open 需要 url")
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" {
		return fmt.Errorf("ws.open 的地址不是合法 URL：%s", trimmed)
	}
	switch strings.ToLower(u.Scheme) {
	case "ws", "wss":
	default:
		return fmt.Errorf("ws.open 只支持 ws/wss：%s", u.Scheme)
	}
	if !plugins.HostAllowed(hosts, u.Host) {
		return fmt.Errorf("ws.open 未声明的地址：%s", u.Host)
	}
	token = strings.TrimSpace(token)
	if len(token) > pluginWSMaxToken {
		return fmt.Errorf("ws.open 的 token 超过 %d 字符", pluginWSMaxToken)
	}

	header := http.Header{}
	// A reverse-proxy-friendly UA; the protocol side logs it, nothing depends on
	// it, and it is far more useful in a support thread than a browser string.
	header.Set("User-Agent", "PetPC-Plugin/1.0")
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}

	conn := &pluginWSConn{
		pluginID: pluginID,
		rawURL:   trimmed,
		display:  redactURL(trimmed),
		header:   header,
		stop:     make(chan struct{}),
		state:    pluginWSStateConnecting,
		since:    time.Now(),
	}

	h.mu.Lock()
	old := h.conns[pluginID]
	h.conns[pluginID] = conn
	h.mu.Unlock()

	// Replacing an existing connection is the plugin saying "start over": the old
	// one is stopped first so two sockets never exist for one plugin id.
	if old != nil {
		old.shutdown()
	}

	go h.supervise(conn)
	return nil
}

// supervise is the whole lifecycle: dial, read, fail, back off, repeat.
//
// It exits when the connection is closed (h.close, h.closeAll, or a replacement)
// or when the plugin stops being installed+enabled. Every exit path that is not a
// close() has to remove the map entry, or a disabled plugin would leave a zombie
// row behind for the next open to replace.
func (h *pluginWSHub) supervise(c *pluginWSConn) {
	attempt := 0
	for {
		if c.stopped() {
			return
		}
		if h.alive != nil && !h.alive(c.pluginID) {
			h.forget(c)
			return
		}
		c.setState(pluginWSStateConnecting, "")
		h.emitState(c)

		sock, err := h.dial(c.rawURL, c.header)
		if err != nil {
			if c.stopped() {
				return
			}
			c.setState(pluginWSStateError, redactError(err))
			h.emitState(c)
			if !h.wait(c, h.backoffFor(attempt)) {
				h.forget(c)
				return
			}
			attempt++
			continue
		}

		attempt = 0
		c.setSocket(sock)
		c.setState(pluginWSStateOpen, "")
		h.emitState(c)

		ping := make(chan struct{})
		go h.pingLoop(c, sock, ping)
		readErr := h.readLoop(c, sock)
		close(ping)
		_ = sock.Close()
		c.setSocket(nil)

		if c.stopped() {
			return
		}
		c.setState(pluginWSStateError, redactError(readErr))
		h.emitState(c)
		if !h.wait(c, h.backoffFor(0)) {
			h.forget(c)
			return
		}
	}
}

// forget drops the map entry if this connection is still the current one.
func (h *pluginWSHub) forget(c *pluginWSConn) {
	h.mu.Lock()
	if cur, ok := h.conns[c.pluginID]; ok && cur == c {
		delete(h.conns, c.pluginID)
	}
	h.mu.Unlock()
}

// readLoop forwards every inbound text frame and returns the error that ended it.
//
// Binary frames are dropped rather than forwarded as base64: every protocol this
// capability exists for speaks JSON text, and a "maybe bytes" payload would force
// the plugin to branch on a case that never happens.
func (h *pluginWSHub) readLoop(c *pluginWSConn, sock wsSocket) error {
	sock.SetReadLimit(pluginWSReadLimit)
	for {
		kind, data, err := sock.ReadMessage()
		if err != nil {
			return err
		}
		if kind != websocket.TextMessage {
			continue
		}
		c.noteReceived()
		if h.emit != nil {
			h.emit(c.pluginID, "ws.message", map[string]any{"text": string(data)})
		}
	}
}

// pingLoop keeps the socket alive and, on the same tick, notices that the plugin
// is gone.
//
// The liveness half is not a duplicate of supervise's check: supervise only asks
// between connections, so without this a plugin disabled while its socket was
// healthy would keep its connection (and keep receiving the group messages of a
// bot the user switched off) until the service dropped it.
func (h *pluginWSHub) pingLoop(c *pluginWSConn, sock wsSocket, stop <-chan struct{}) {
	every := h.pingEvery
	if every <= 0 {
		every = pluginWSPingInterval
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-c.stop:
			return
		case <-ticker.C:
			if h.alive != nil && !h.alive(c.pluginID) {
				h.close(c.pluginID)
				return
			}
			c.writeMu.Lock()
			err := sock.WriteControl(websocket.PingMessage, nil, time.Now().Add(pluginWSWriteTimeout))
			c.writeMu.Unlock()
			if err != nil {
				// Let the reader report it: closing here would race the read loop's
				// own error handling and emit two different endings for one drop.
				return
			}
		}
	}
}

// send writes one text frame.
func (h *pluginWSHub) send(pluginID, text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("ws.send 需要 text")
	}
	if len(text) > pluginWSMaxText {
		return fmt.Errorf("ws.send 的 text 超过 %d 字节", pluginWSMaxText)
	}
	h.mu.Lock()
	c := h.conns[pluginID]
	h.mu.Unlock()
	if c == nil {
		return errors.New("还没有打开 WebSocket：先调用 ws.open")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	sock := c.currentSocket()
	if sock == nil || c.currentState() != pluginWSStateOpen {
		return errors.New("WebSocket 还没连上")
	}
	_ = sock.SetWriteDeadline(time.Now().Add(pluginWSWriteTimeout))
	if err := sock.WriteMessage(websocket.TextMessage, []byte(text)); err != nil {
		return fmt.Errorf("ws.send 失败：%w", err)
	}
	c.noteSent()
	return nil
}

// close stops a plugin's connection.
func (h *pluginWSHub) close(pluginID string) {
	h.mu.Lock()
	c := h.conns[pluginID]
	delete(h.conns, pluginID)
	h.mu.Unlock()
	if c == nil {
		return
	}
	c.shutdown()
	h.emitState(c)
}

// closeAll stops every connection, without emitting: it runs on shutdown, when
// there is no page left to receive anything.
func (h *pluginWSHub) closeAll() {
	h.mu.Lock()
	conns := make([]*pluginWSConn, 0, len(h.conns))
	for _, c := range h.conns {
		conns = append(conns, c)
	}
	h.conns = map[string]*pluginWSConn{}
	h.mu.Unlock()
	for _, c := range conns {
		c.shutdown()
	}
}

// state returns one plugin's connection view, or the zero value with closed.
func (h *pluginWSHub) state(pluginID string) pluginWSView {
	h.mu.Lock()
	c := h.conns[pluginID]
	h.mu.Unlock()
	if c == nil {
		return pluginWSView{State: pluginWSStateClosed}
	}
	return c.view()
}

// --- redaction --------------------------------------------------------------

// redactURL keeps the scheme, host and path and replaces every query value with
// "***".
//
// The whole reason ws.open takes a URL rather than host+port+path+token is that
// the protocols this serves put their token in the query string
// (`/event?access_token=...`). That means the string reaches a plugin-authored UI
// and our own event stream, and neither should be able to carry the credential to
// a screenshot.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "***"
	}
	if u.RawQuery == "" {
		return raw
	}
	parts := strings.Split(u.RawQuery, "&")
	for i, p := range parts {
		if eq := strings.Index(p, "="); eq >= 0 {
			parts[i] = p[:eq] + "=***"
		} else if p != "" {
			parts[i] = p + "=***"
		}
	}
	u.RawQuery = strings.Join(parts, "&")
	return u.String()
}

// redactError trims a dial/read error to something a UI can show, and strips any
// URL query it may have echoed.
func redactError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if i := strings.Index(msg, "://"); i >= 0 {
		if j := strings.IndexAny(msg[i:], " \n\t"); j > 0 {
			msg = msg[:i] + redactURL(msg[i:i+j]) + msg[i+j:]
		}
	}
	const maxErr = 300
	runes := []rune(msg)
	if len(runes) > maxErr {
		msg = string(runes[:maxErr]) + "…"
	}
	return msg
}

// --- App wiring -------------------------------------------------------------

// pluginWSHubLocked opens the hub on first use.
//
// Lazy for the same reason the manager is: a plugin subsystem that cannot start
// must not be able to stop the pet from appearing.
func (a *App) pluginWS() *pluginWSHub {
	a.pluginHost.mu.Lock()
	defer a.pluginHost.mu.Unlock()
	if a.pluginHost.ws == nil {
		a.pluginHost.ws = newPluginWSHub(
			func(pluginID, name string, payload any) {
				a.emitPluginEvent(pluginID, name, payload)
			},
			func(pluginID string) bool {
				row, ok := a.pluginManager().Get(pluginID)
				return ok && row.Enabled
			},
		)
	}
	return a.pluginHost.ws
}

// closePluginWS stops every connection. Called from shutdown.
func (a *App) closePluginWS() {
	a.pluginHost.mu.Lock()
	hub := a.pluginHost.ws
	a.pluginHost.mu.Unlock()
	if hub != nil {
		hub.closeAll()
	}
}

// pluginWSHosts is the allow-list for one plugin: the manifest's static hosts
// plus whatever its host-whitelist setting currently says.
//
// It is the same expression net.fetch uses, deliberately: two spellings of "where
// may this plugin go" would eventually disagree, and the disagreement would look
// like a host that is reachable one way and not the other.
func (a *App) pluginWSHosts(id string, man plugins.Manifest) []string {
	return append(append([]string{}, man.NetHosts...),
		plugins.ConfigHosts(man, a.pluginConfigValues(id))...)
}

// pluginWSOpen is the ws.open capability.
func pluginWSOpen(a *App, id string, payload []byte) (any, error) {
	row, ok := a.pluginManager().Get(id)
	if !ok || row.Manifest == nil {
		return nil, fmt.Errorf("插件 %s 未启用", id)
	}
	var in struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if err := decodePayload(payload, &in); err != nil {
		return nil, err
	}
	if err := a.pluginWS().open(id, in.URL, in.Token, a.pluginWSHosts(id, *row.Manifest)); err != nil {
		return nil, err
	}
	view := a.pluginWS().state(id)
	return map[string]any{"ok": true, "state": view.State, "url": view.URL}, nil
}

// pluginWSSend is the ws.send capability.
func pluginWSSend(a *App, id string, payload []byte) (any, error) {
	var in struct {
		Text string `json:"text"`
	}
	if err := decodePayload(payload, &in); err != nil {
		return nil, err
	}
	if err := a.pluginWS().send(id, in.Text); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// pluginWSClose is the ws.close capability. Closing something already closed
// succeeds: "make sure I have no connection" is the intent, and reporting a
// missing socket as an error would make a plugin's teardown path fail for having
// run twice.
func pluginWSClose(a *App, id string, _ []byte) (any, error) {
	a.pluginWS().close(id)
	return map[string]any{"ok": true, "state": pluginWSStateClosed}, nil
}

// pluginWSState is the ws.state capability.
func pluginWSState(a *App, id string, _ []byte) (any, error) {
	return a.pluginWS().state(id), nil
}
