package main

// net.fetch, in the market program.
//
// It exists here for the same reason it exists in the pet: a plugin that talks to
// a service the user runs (the voice plugin's GPT-SoVITS engine) needs to make an
// HTTP request, and the host is what holds the allow-list.
//
// The rules are the pet's rules, deliberately and in the same order:
//
//  1. the host must be on the manifest's static netHosts OR in the value the user
//     typed into the declared host-whitelist setting;
//  2. only http(s), only GET/POST;
//  3. the body is text unless the plugin asks for base64, because the text form is
//     LOSSY for anything that is not valid UTF-8 - a WAV came back as the same
//     length of U+FFFD replacement characters, which looks like success and plays
//     as noise.
//
// The check happens BEFORE the request is built, so a refused host never reaches
// the network: a plugin cannot use a rejection as a probe for what is listening.

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"PetPC/internal/plugins"
)

// The response cap is plugins.MaxFetchResponseBytes.
//
// It used to be a local `maxFetchBytes = 1 << 20` that "matched the pet's limit" - and did not,
// because the pet's had been raised to 8 MiB when the QQ sticker library showed what a 1 MiB
// ceiling costs (156 of 317 images cut silently). Two numbers for one documented limit is how a
// plugin downloads a picture in the 插件 page and gets half of it.

func marketNetFetch(host any, call plugins.Call, payload []byte) (any, error) {
	m := host.(*Market)
	var in struct {
		Method   string `json:"method"`
		URL      string `json:"url"`
		Body     string `json:"body"`
		Encoding string `json:"encoding"`
		// BodyBase64 is the request body as bytes. The rule (one body, never two;
		// 16 MiB; padded or unpadded) is plugins.RequestBody, shared with the pet
		// deliberately: the 插件 page mounts the same documents, and a plugin whose
		// upload works in one window and not the other is a bug nobody can read.
		BodyBase64 string `json:"bodyBase64"`
		// Headers are honoured here for the same reason: a plugin's page is where it
		// EDITS its settings and (for a tool like an image host) does the work, and an
		// upload endpoint needs `content-type` to see the multipart boundary while an
		// API behind a token needs `authorization`. This handler used to accept the
		// field and ignore it, which read as "the server said no file" to the plugin -
		// the closed list and the validation are plugins.ApplyFetchHeaders, the pet's.
		Headers map[string]string `json:"headers"`
	}
	if err := plugins.DecodePayload(payload, &in); err != nil {
		return nil, err
	}
	enc := strings.ToLower(strings.TrimSpace(in.Encoding))
	if enc != "" && enc != "text" && enc != "base64" {
		return nil, fmt.Errorf("net.fetch 的 encoding 只支持 text/base64：%s", in.Encoding)
	}
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet && method != http.MethodPost {
		return nil, fmt.Errorf("net.fetch 只支持 GET/POST：%s", method)
	}

	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("net.fetch 只支持 http/https：%s", in.URL)
	}
	// The allow-list is the manifest's static netHosts PLUS whatever the user typed
	// into the declared host-whitelist setting. The configured value is read from
	// the plugin's OWN config store, which this program is the editor for.
	values := map[string]string{}
	if st := plugins.NewConfigStore(call.PrivateDir, call.Manifest, m.keeper); st != nil {
		if read, err := st.Read(); err == nil {
			values = read.Values
		}
	}
	hosts := append(append([]string{}, call.Manifest.NetHosts...),
		plugins.ConfigHosts(call.Manifest, values)...)
	if !plugins.HostAllowed(hosts, u.Host) {
		return nil, fmt.Errorf("net.fetch 未声明的域名：%s", u.Host)
	}

	bodyBytes, err := plugins.RequestBody(in.Body, in.BodyBase64)
	if err != nil {
		return nil, err
	}
	var body io.Reader
	if bodyBytes != nil {
		body = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequest(method, in.URL, body)
	if err != nil {
		return nil, err
	}
	// After the host check, so a refused host never receives a credential a header
	// may carry; before the request, so a refused header is not a request that
	// half-travelled.
	if err := plugins.ApplyFetchHeaders(req.Header, in.Headers); err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// +1 so "exactly at the cap" and "over it" are distinguishable; the shared cap is
	// MaxFetchResponseBytes because one documented limit serving two programs must not be
	// two numbers (see that constant's comment: this handler used to cut at 1 MiB in silence).
	data, err := io.ReadAll(io.LimitReader(resp.Body, plugins.MaxFetchResponseBytes+1))
	if err != nil {
		return nil, err
	}
	out := map[string]any{"status": resp.StatusCode}
	if len(data) > plugins.MaxFetchResponseBytes {
		data = data[:plugins.MaxFetchResponseBytes]
		out["truncated"] = true
	}
	if enc == "base64" {
		out["body"] = base64.StdEncoding.EncodeToString(data)
		out["encoding"] = "base64"
	} else {
		out["body"] = string(data)
	}
	return out, nil
}
