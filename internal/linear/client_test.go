package linear

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientDo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		resp := map[string]any{"data": map[string]any{"viewer": map[string]any{"id": "u1"}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("token")
	client.Endpoint = server.URL
	var out struct {
		Viewer struct {
			ID string `json:"id"`
		} `json:"viewer"`
	}
	if err := client.Do(context.Background(), queryViewer, nil, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if out.Viewer.ID != "u1" {
		t.Fatalf("expected id u1, got %q", out.Viewer.ID)
	}
}
