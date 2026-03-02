package handlers

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/andyrewlee/medusa/internal/logging"
	"github.com/gorilla/websocket"
)

// ptyWSMessage is a client-to-server PTY WebSocket message.
type ptyWSMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"` // base64-encoded for input
	Rows int    `json:"rows,omitempty"`
	Cols int    `json:"cols,omitempty"`
}

// TabPTYWebSocket handles WebSocket connections for PTY-based agent tabs.
func (h *Handlers) TabPTYWebSocket(w http.ResponseWriter, r *http.Request) {
	tabID := r.PathValue("tabID")

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logging.Warn("PTY WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	// Subscribe to PTY output
	outputCh, unsub := h.svc.Agents.SubscribePTYOutput(tabID)
	if outputCh == nil {
		sendWSError(conn, "tab not found or not a PTY tab: "+tabID)
		return
	}
	defer unsub()

	var writeMu sync.Mutex

	// Read loop: handle client input
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, msgBytes, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg ptyWSMessage
			if err := json.Unmarshal(msgBytes, &msg); err != nil {
				continue
			}

			switch msg.Type {
			case "input":
				data, err := base64.StdEncoding.DecodeString(msg.Data)
				if err != nil {
					continue
				}
				h.svc.Agents.SendInput(tabID, data)
			case "resize":
				if msg.Rows > 0 && msg.Cols > 0 {
					h.svc.Agents.ResizeTerminal(tabID, uint16(msg.Rows), uint16(msg.Cols))
				}
			}
		}
	}()

	// Write loop: forward PTY output to client as binary frames
	for {
		select {
		case <-done:
			return
		case data, ok := <-outputCh:
			if !ok {
				return
			}
			writeMu.Lock()
			err := conn.WriteMessage(websocket.BinaryMessage, data)
			writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}
}
