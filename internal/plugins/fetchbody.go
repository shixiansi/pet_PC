package plugins

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// MaxFetchUploadBytes bounds ONE request body sent through `net.fetch`.
//
// It is a per-request ceiling, like the response cap next to it, and it is set by what
// this capability is FOR: putting a file somewhere (an image to an image host, a clip to
// a synthesis engine). 16 MiB is the limit the audio directory already uses for one clip,
// so a plugin that can save a file can also send it - and a phone photo with room to
// spare. Larger than that is a download manager's job, not a chat with a pet.
//
// The number on the wire is 4/3 of this (base64), which is why the check runs twice:
// once on the encoded length before anything is allocated, once on the real bytes.
const MaxFetchUploadBytes = 16 << 20

// MaxFetchResponseBytes bounds ONE response body read by `net.fetch`.
//
// Shared for the same reason as the request rule above, and it is a correction rather than a
// nicety: the control center's own copy of the handler used to stop at 1 MiB and say nothing,
// while the pet read 8 MiB and set `truncated`. The plugin manual documents ONE limit ("8 MiB,
// `truncated: true` when it is cut"), so a plugin that downloads a picture into its page - the
// QQ sticker library, the bilibili cover - got a silently halved image whenever it happened to
// be running in the 插件 page. A ceiling nobody can detect is the bug this constant prevents.
const MaxFetchResponseBytes = 8 << 20

// RequestBody is the ONE rule for what `net.fetch` sends, shared by both hosts.
//
// Two fields, one of them:
//
//   - `body` is a STRING. It is what a JSON API or a form POST needs.
//   - `bodyBase64` is BYTES. It exists because a string that arrives through
//     encoding/json is LOSSY for anything that is not valid UTF-8: the same trap
//     `encoding: "base64"` already fixes on the RESPONSE side. Without it a plugin
//     cannot send a file at all - an image or a WAV comes out with U+FFFD where the
//     bytes were, which looks like success and lands as a corrupt file at the far end.
//     That is the reason this exists at all: "upload a picture" is not expressible
//     with a text body, and the alternative (a sidecar script, or a new permission
//     just to POST a file) would make a network feature ask the user to trust a
//     local program.
//
// Both at once is REFUSED rather than merged or prioritised. Two bodies in one request
// is a plugin bug, and "which one did it actually send" must not be a guess - the same
// reason `body` and `encoding` are checked before the request is built.
//
// It lives here rather than in each program because BOTH serve net.fetch (they mount
// the same plugin documents). Two copies of this rule is how "the upload works beside
// the pet and fails in the control center" starts, and a plugin author cannot debug it
// from the error text.
func RequestBody(textBody, base64Body string) ([]byte, error) {
	hasText := strings.TrimSpace(textBody) != ""
	hasBytes := strings.TrimSpace(base64Body) != ""
	if hasText && hasBytes {
		return nil, fmt.Errorf("net.fetch 的 body 与 bodyBase64 只能给一个（一个是文本，一个是字节）")
	}
	if !hasBytes {
		if !hasText {
			return nil, nil
		}
		return []byte(textBody), nil
	}
	// Checked on the ENCODED length first: decoding a 500 MB string to then reject it
	// would allocate the thing the cap exists to prevent.
	if len(base64Body)/4*3 > MaxFetchUploadBytes+3 {
		return nil, fmt.Errorf("net.fetch 的请求体超过上限 %d 字节（base64 解出来会更大）", MaxFetchUploadBytes)
	}
	raw, err := base64.StdEncoding.DecodeString(base64Body)
	if err != nil {
		// Padded and unpadded both occur in the wild: `btoa` pads, some HTTP clients do
		// not. A base64 body that fails only because of the padding would be an error
		// message about base64 that has no actionable content.
		raw, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(base64Body, "="))
	}
	if err != nil {
		return nil, fmt.Errorf("net.fetch 的 bodyBase64 不是合法 base64：%w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("net.fetch 的 bodyBase64 解出了空请求体")
	}
	if len(raw) > MaxFetchUploadBytes {
		return nil, fmt.Errorf("net.fetch 的请求体超过上限 %d 字节", MaxFetchUploadBytes)
	}
	return raw, nil
}
