package capture

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
)

// WebViewPath is where the WebView adapter reports to Go. Mount
// WebViewHandler there for POST next to the renderer's action route:
//
//	app.Router().Post(capture.WebViewPath, capture.WebViewHandler())
const WebViewPath = "/_capture/device"

// maxWebViewBatch bounds one report: about two seconds of 50 Mbit/s video.
const maxWebViewBatch = 16 << 20

// webViewBatch is one report's header. Samples' bytes follow the header in
// the order the items list them.
type webViewBatch struct {
	Session string `json:"session"`
	Items   []struct {
		Event   string `json:"event,omitempty"`
		Payload string `json:"payload,omitempty"`
		Track   int    `json:"track"`
		Flags   int    `json:"flags"`
		Clock   int64  `json:"clock"`
		Size    int    `json:"size"`
	} `json:"items"`
}

// WebViewHandler is the WebView adapter's way to the session: the same events
// and samples the Android adapter delivers through its Java binding. The body
// is a 4-byte big-endian header length, the JSON header, then the samples'
// bytes. The answer is the session's directives as text.
//
// It accepts reports only for the current session. The app's own page is the
// only caller: the WebView shell serves the app origin from this process and
// opens no listener.
func WebViewHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxWebViewBatch+1))
		if err != nil || len(body) > maxWebViewBatch || len(body) < 4 {
			http.Error(w, "invalid capture report", http.StatusBadRequest)
			return
		}
		length := int64(binary.BigEndian.Uint32(body))
		if length > int64(len(body)-4) {
			http.Error(w, "invalid capture report", http.StatusBadRequest)
			return
		}
		var batch webViewBatch
		if err := json.Unmarshal(body[4:4+length], &batch); err != nil || !validID(batch.Session) || len(batch.Items) > 4096 {
			http.Error(w, "invalid capture report", http.StatusBadRequest)
			return
		}
		data := body[4+length:]
		total := 0
		for _, item := range batch.Items {
			// Compared against what is left, so huge sizes cannot overflow.
			if item.Size < 0 || item.Size > len(data)-total || item.Event != "" && item.Size != 0 {
				http.Error(w, "invalid capture report", http.StatusBadRequest)
				return
			}
			total += item.Size
		}
		if total != len(data) {
			http.Error(w, "invalid capture report", http.StatusBadRequest)
			return
		}
		var directives []string
		note := func(answer string) {
			for _, directive := range strings.Split(answer, ",") {
				if directive == "" {
					continue
				}
				// A later bitrate replaces an earlier one.
				if strings.HasPrefix(directive, "bitrate:") {
					directives = slices.DeleteFunc(directives, func(seen string) bool { return strings.HasPrefix(seen, "bitrate:") })
				} else if slices.Contains(directives, directive) {
					continue
				}
				directives = append(directives, directive)
			}
		}
		if len(batch.Items) == 0 {
			note(Event(batch.Session, "heartbeat", ""))
		}
		for _, item := range batch.Items {
			if item.Event != "" {
				note(Event(batch.Session, item.Event, item.Payload))
				continue
			}
			note(Sample(batch.Session, item.Track, item.Flags, item.Clock, data[:item.Size]))
			data = data[item.Size:]
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		io.WriteString(w, strings.Join(directives, ","))
	})
}

// WebViewClosed ends the current session when the WebView that ran it is
// gone, without waiting for a report the page can no longer send. Go writes
// a WebView recording itself, so it keeps everything that arrived. A host
// calls it as it shuts down; without it the session stays Stopping and
// blocks the next one for the life of the process.
func WebViewClosed() {
	if session := Current(); session != nil {
		session.finish(Stopped, "closed", "The page that ran capture closed.")
	}
}
