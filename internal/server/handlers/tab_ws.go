package handlers

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/andyrewlee/medusa/internal/logging"
	"github.com/andyrewlee/medusa/internal/service"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// wsMessage is a client-to-server WebSocket message.
type wsMessage struct {
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	ToolUseID  string `json:"tool_use_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// wsServerMessage is a server-to-client WebSocket message.
type wsServerMessage struct {
	Type      string              `json:"type"`
	TabID     string              `json:"tab_id,omitempty"`
	State     string              `json:"state,omitempty"`
	SessionID string              `json:"session_id,omitempty"`
	Data      *service.SDKMessage `json:"data,omitempty"`
	Messages  []service.SDKMessage `json:"messages,omitempty"`
	TotalCost float64             `json:"total_cost_usd,omitempty"`
	Reason    string              `json:"reason,omitempty"`
	Error     string              `json:"error,omitempty"`
}

// TabWebSocket handles WebSocket connections for Claude tabs.
func (h *Handlers) TabWebSocket(w http.ResponseWriter, r *http.Request) {
	tabID := r.PathValue("tabID")

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logging.Warn("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	// Get current tab state
	tabState, err := h.svc.Claude.GetTabState(tabID)
	if err != nil {
		sendWSError(conn, "tab not found: "+tabID)
		return
	}

	// Send connected message
	sendWSJSON(conn, wsServerMessage{
		Type:      "connected",
		TabID:     tabID,
		State:     string(tabState.State),
		SessionID: tabState.SessionID,
	})

	// Send history
	history, err := h.svc.Sessions.LoadHistory(tabID)
	if err == nil && len(history) > 0 {
		sendWSJSON(conn, wsServerMessage{
			Type:     "history",
			Messages: history,
		})
	}

	// Subscribe to tab events
	subID := "ws-" + tabID + "-" + generateSubID()
	eventCh := h.svc.EventBus.Subscribe(subID, 256)
	defer h.svc.EventBus.Unsubscribe(subID)

	var writeMu sync.Mutex

	// Read loop: handle client messages
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, msgBytes, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg wsMessage
			if err := json.Unmarshal(msgBytes, &msg); err != nil {
				continue
			}

			switch msg.Type {
			case "prompt":
				if err := h.svc.Claude.SendPrompt(tabID, msg.Text); err != nil {
					writeMu.Lock()
					sendWSJSON(conn, wsServerMessage{Type: "error", Error: err.Error()})
					writeMu.Unlock()
				}
			case "interrupt":
				if err := h.svc.Claude.Interrupt(tabID); err != nil {
					writeMu.Lock()
					sendWSJSON(conn, wsServerMessage{Type: "error", Error: err.Error()})
					writeMu.Unlock()
				}
			case "ping":
				writeMu.Lock()
				sendWSJSON(conn, wsServerMessage{Type: "pong"})
				writeMu.Unlock()
			}
		}
	}()

	// Write loop: forward events to client
	for {
		select {
		case <-done:
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			// Filter for events relevant to this tab
			serverMsg := filterTabEvent(tabID, event)
			if serverMsg == nil {
				continue
			}
			writeMu.Lock()
			if err := sendWSJSON(conn, serverMsg); err != nil {
				writeMu.Unlock()
				return
			}
			writeMu.Unlock()
		}
	}
}

func filterTabEvent(tabID string, event service.Event) *wsServerMessage {
	switch event.Type {
	case service.EventTabMessageReceived:
		// Decode payload to check tab ID
		var payload struct {
			TabID   string             `json:"tab_id"`
			Message service.SDKMessage `json:"message"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return nil
		}
		if payload.TabID != tabID {
			return nil
		}
		return &wsServerMessage{
			Type: "message",
			Data: &payload.Message,
		}

	case service.EventTabStateChanged:
		var payload struct {
			TabID string `json:"tab_id"`
			State string `json:"state"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return nil
		}
		if payload.TabID != tabID {
			return nil
		}
		return &wsServerMessage{
			Type:  "state_changed",
			State: payload.State,
		}

	case service.EventTabClosed:
		var payload struct {
			TabID  string `json:"tab_id"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return nil
		}
		if payload.TabID != tabID {
			return nil
		}
		return &wsServerMessage{
			Type:   "closed",
			Reason: payload.Reason,
		}
	}
	return nil
}

func sendWSJSON(conn *websocket.Conn, v any) error {
	return conn.WriteJSON(v)
}

func sendWSError(conn *websocket.Conn, msg string) {
	conn.WriteJSON(wsServerMessage{Type: "error", Error: msg})
}

func generateSubID() string {
	// Simple unique ID for subscription deduplication
	b := make([]byte, 8)
	for i := range b {
		b[i] = "abcdefghijklmnopqrstuvwxyz0123456789"[i%36]
	}
	return string(b)
}
