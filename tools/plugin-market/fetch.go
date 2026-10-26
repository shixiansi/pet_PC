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
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"PetPC/internal/plugins"
)

// maxFetchBytes bounds one response, matching the pet's limit.
const maxFetchBytes = 1 << 20

func marketNetFetch(host any, call plugins.Call, payload []byte) (any, error) {
	m := host.(*Market)
	var in struct {
		Method   string `json:"method"`
		URL      string `json:"url"`
		Body     string `json:"body"`
		Encoding string `json:"encoding"`
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

	var body io.Reader
	if in.Body != "" {
		body = strings.NewReader(in.Body)
	}
	req, err := http.NewRequest(method, in.URL, body)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes))
	if err != nil {
		return nil, err
	}
	out := map[string]any{"status": resp.StatusCode}
	if enc == "base64" {
		out["body"] = base64.StdEncoding.EncodeToString(data)
		out["encoding"] = "base64"
	} else {
		out["body"] = string(data)
	}
	return out, nil
}
