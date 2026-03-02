package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/andyrewlee/medusa/internal/logging"
)

// SSEEvents handles Server-Sent Events for global state changes.
func (h *Handlers) SSEEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	subID := "sse-" + generateSubID()
	eventCh := h.svc.EventBus.Subscribe(subID, 128)
	defer h.svc.EventBus.Unsubscribe(subID)

	// Send initial ping
	fmt.Fprintf(w, "data: {\"type\":\"connected\"}\n\n")
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}

			data, err := json.Marshal(event)
			if err != nil {
				logging.Warn("SSE marshal error: %v", err)
				continue
			}

			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
