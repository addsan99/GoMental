package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"GoMental/internal/apphost"
)

// handleEvents streams core events to the client as Server-Sent Events. Each
// HTTP connection becomes exactly one hub subscriber; the subscription is torn
// down when the request context is cancelled (client disconnect) so no goroutine
// or channel leaks. Payloads are JSON-encoded in the SSE `data:` field and the
// event name is carried in the SSE `event:` field so browsers can use
// addEventListener(name, ...).
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErrorStatus(w, http.StatusInternalServerError, "events.unsupported", "streaming is not supported by this server")
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // disable proxy buffering (nginx)

	sub := s.host.Hub().Subscribe(0)
	defer sub.Close()

	// Prime the stream so the client's EventSource fires `open` promptly and any
	// intermediary flushes headers.
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.Events():
			if !ok {
				return
			}
			writeSSE(w, ev)
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, ev apphost.Event) {
	payload, err := json.Marshal(ev.Payload)
	if err != nil {
		payload = []byte("null")
	}
	// event: <name>\ndata: <json>\n\n
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Name, payload)
}
