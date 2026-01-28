package linear

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// WebhookEvent represents a parsed webhook event.
type WebhookEvent struct {
	Account string
	Type    string
	Action  string
	Data    json.RawMessage
}

// WebhookServer runs a local HTTP server for Linear webhooks.
type WebhookServer struct {
	Addr    string
	Secrets map[string]string
	Handler func(event WebhookEvent)
}

// NewWebhookServer creates a new webhook server.
func NewWebhookServer(addr string, secrets map[string]string, handler func(event WebhookEvent)) *WebhookServer {
	return &WebhookServer{
		Addr:    addr,
		Secrets: secrets,
		Handler: handler,
	}
}

// Start launches the webhook server until ctx is canceled.
func (s *WebhookServer) Start(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("webhook server not initialized")
	}
	if s.Addr == "" {
		return fmt.Errorf("webhook server address required")
	}
	if len(s.Secrets) == 0 {
		return fmt.Errorf("webhook server secrets required")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/linear/webhook", s.handleWebhook)
	server := &http.Server{Addr: s.Addr, Handler: mux}

	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()

	return server.ListenAndServe()
}

type webhookPayload struct {
	Type   string          `json:"type"`
	Action string          `json:"action"`
	Data   json.RawMessage `json:"data"`
}

func (s *WebhookServer) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	signature := r.Header.Get("Linear-Signature")
	timestamp := r.Header.Get("Linear-Timestamp")
	if signature == "" || timestamp == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !validateTimestamp(timestamp) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	account := ""
	for name, secret := range s.Secrets {
		if VerifySignature(secret, raw, signature) {
			account = name
			break
		}
	}
	if account == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	var payload webhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if s.Handler != nil {
		s.Handler(WebhookEvent{
			Account: account,
			Type:    payload.Type,
			Action:  payload.Action,
			Data:    payload.Data,
		})
	}

	w.WriteHeader(http.StatusOK)
}

func validateTimestamp(raw string) bool {
	// Accept epoch seconds.
	sec, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return false
	}
	ts := time.Unix(sec, 0)
	now := time.Now()
	if ts.After(now.Add(5 * time.Minute)) {
		return false
	}
	if ts.Before(now.Add(-5 * time.Minute)) {
		return false
	}
	return true
}
