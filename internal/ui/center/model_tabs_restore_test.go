package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

func TestAddDetachedTab_SetsLastFocusedFromCreatedAt(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	createdAt := time.Now().Add(-time.Hour).Unix()

	m.addDetachedTab(ws, data.TabInfo{
		Assistant:   "claude",
		Name:        "Claude",
		SessionName: "sess-detached",
		CreatedAt:   createdAt,
	})

	tabs := m.tabsByWorkspace[wsID]
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tabs))
	}
	if tabs[0].lastFocusedAt != time.Unix(createdAt, 0) {
		t.Fatalf("expected lastFocusedAt=%s, got %s", time.Unix(createdAt, 0), tabs[0].lastFocusedAt)
	}
}

func TestAddPlaceholderTab_SetsLastFocusedFromCreatedAt(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	createdAt := time.Now().Add(-2 * time.Hour).Unix()

	_, _ = m.addPlaceholderTab(ws, data.TabInfo{
		Assistant: "claude",
		Name:      "Claude",
		CreatedAt: createdAt,
	})

	tabs := m.tabsByWorkspace[wsID]
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tabs))
	}
	if tabs[0].lastFocusedAt != time.Unix(createdAt, 0) {
		t.Fatalf("expected lastFocusedAt=%s, got %s", time.Unix(createdAt, 0), tabs[0].lastFocusedAt)
	}
}
