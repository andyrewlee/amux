package apiclient

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/andyrewlee/medusa/internal/logging"
	"github.com/gorilla/websocket"
)

// WSMessage is a server-to-client WebSocket message.
type WSMessage struct {
	Type      string               `json:"type"`
	TabID     string               `json:"tab_id,omitempty"`
	State     string               `json:"state,omitempty"`
	SessionID string               `json:"session_id,omitempty"`
	Data      *SDKMessageResponse  `json:"data,omitempty"`
	Messages  []SDKMessageResponse `json:"messages,omitempty"`
	TotalCost float64              `json:"total_cost_usd,omitempty"`
	Reason    string               `json:"reason,omitempty"`
	Error     string               `json:"error,omitempty"`
}

// TabWSConn manages a WebSocket connection to a Claude tab.
type TabWSConn struct {
	conn     *websocket.Conn
	url      string
	mu       sync.Mutex
	closed   bool
	handlers []func(WSMessage)
}

// ConnectTabWS establishes a WebSocket connection to a Claude tab.
func (c *Client) ConnectTabWS(tabID string) (*TabWSConn, error) {
	wsURL := c.TabWSURL(tabID)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return nil, err
	}

	tc := &TabWSConn{
		conn: conn,
		url:  wsURL,
	}

	go tc.readLoop()
	return tc, nil
}

// OnMessage registers a handler for incoming WebSocket messages.
func (tc *TabWSConn) OnMessage(handler func(WSMessage)) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.handlers = append(tc.handlers, handler)
}

// SendPrompt sends a user prompt via WebSocket.
func (tc *TabWSConn) SendPrompt(text string) error {
	return tc.send(map[string]string{"type": "prompt", "text": text})
}

// SendInterrupt sends an interrupt signal via WebSocket.
func (tc *TabWSConn) SendInterrupt() error {
	return tc.send(map[string]string{"type": "interrupt"})
}

// Close terminates the WebSocket connection.
func (tc *TabWSConn) Close() error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.closed = true
	if tc.conn != nil {
		return tc.conn.Close()
	}
	return nil
}

func (tc *TabWSConn) send(v any) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if tc.closed || tc.conn == nil {
		return nil
	}
	return tc.conn.WriteJSON(v)
}

func (tc *TabWSConn) readLoop() {
	defer tc.Close()
	for {
		_, msgBytes, err := tc.conn.ReadMessage()
		if err != nil {
			if !tc.closed {
				logging.Warn("Tab WS read error: %v", err)
			}
			return
		}

		var msg WSMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			continue
		}

		tc.mu.Lock()
		handlers := make([]func(WSMessage), len(tc.handlers))
		copy(handlers, tc.handlers)
		tc.mu.Unlock()

		for _, h := range handlers {
			h(msg)
		}
	}
}

// SSEConn manages a Server-Sent Events connection.
type SSEConn struct {
	client  *Client
	closed  bool
	mu      sync.Mutex
	handler func(json.RawMessage)
}

// ConnectSSE establishes an SSE connection for global events.
func (c *Client) ConnectSSE(handler func(json.RawMessage)) (*SSEConn, error) {
	sc := &SSEConn{
		client:  c,
		handler: handler,
	}
	go sc.readLoop()
	return sc, nil
}

func (sc *SSEConn) readLoop() {
	for !sc.isClosed() {
		resp, err := sc.client.HTTP.Get(sc.client.SSEURL())
		if err != nil {
			logging.Warn("SSE connection error: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}

		scanner := json.NewDecoder(resp.Body)
		for scanner.More() {
			var raw json.RawMessage
			if err := scanner.Decode(&raw); err != nil {
				break
			}
			if sc.handler != nil {
				sc.handler(raw)
			}
		}
		resp.Body.Close()

		if !sc.isClosed() {
			time.Sleep(3 * time.Second)
		}
	}
}

func (sc *SSEConn) isClosed() bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.closed
}

// Close terminates the SSE connection.
func (sc *SSEConn) Close() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.closed = true
}
