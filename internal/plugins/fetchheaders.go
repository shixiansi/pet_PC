package plugins

import (
	"fmt"
	"net/http"
	"strings"
)

// FetchHeaders is the closed set of request headers a plugin may set through net.fetch.
//
// It is a list rather than "anything the plugin wants" because a header is a way to say
// things the host allow-list does not: `Host` picks a virtual host, `Cookie` and `Origin`
// carry ambient credentials, and `Content-Length`/`Transfer-Encoding` can contradict the
// body the host is about to send. These four are what an API behind a token actually needs
// - `content-type` and `authorization` are the pair a JSON API or a multipart upload asks
// for - and nothing here can aim the request at a host that was not checked.
//
// `content-type` is also load-bearing for `bodyBase64` (see fetchbody.go): a multipart body
// is only a multipart body if the boundary travels in this header, so an upload endpoint
// would answer "no file" without it.
//
// It lives here, in internal/plugins, because BOTH programs serve net.fetch and a header
// list that exists twice is how a plugin ends up able to authenticate beside the pet and
// unable to in the control center - with no error message that says so.
var FetchHeaders = map[string]bool{
	"accept":        true,
	"authorization": true,
	"content-type":  true,
	"user-agent":    true,
}

// MaxFetchHeaderValue bounds one header value.
const MaxFetchHeaderValue = 1024

// ApplyFetchHeader validates one header from a plugin and sets it on the request.
//
// Refusal rather than dropping: a plugin that asked for a header it may not have, and
// silently did not get, builds a request that fails at the far end for a reason nothing in
// this process knows about. The error text names the four that are allowed, because that is
// the fact the author needs.
func ApplyFetchHeader(header http.Header, name, value string) error {
	key := strings.ToLower(strings.TrimSpace(name))
	if !FetchHeaders[key] {
		return fmt.Errorf("net.fetch 不允许设置请求头 %q（只支持 accept / authorization / content-type / user-agent）", name)
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("net.fetch 的请求头 %s 含换行", key)
	}
	if len(value) > MaxFetchHeaderValue {
		return fmt.Errorf("net.fetch 的请求头 %s 超过 %d 字节", key, MaxFetchHeaderValue)
	}
	header.Set(key, value)
	return nil
}

// ApplyFetchHeaders applies a whole header map, stopping at the first refusal.
//
// The whole map is refused rather than partially applied, and that is the same rule as the
// permission gate: a request that half-happened is the state a plugin cannot reason about.
func ApplyFetchHeaders(header http.Header, values map[string]string) error {
	for name, value := range values {
		if err := ApplyFetchHeader(header, name, value); err != nil {
			return err
		}
	}
	return nil
}
