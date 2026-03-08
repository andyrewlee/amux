package center

import (
	"sync/atomic"
	"testing"

	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestGenerateTabID_StaysUniqueAcrossCounterReset(t *testing.T) {
	oldPrefix := tabIDPrefix
	oldCounter := atomic.LoadUint64(&tabIDCounter)
	t.Cleanup(func() {
		tabIDPrefix = oldPrefix
		atomic.StoreUint64(&tabIDCounter, oldCounter)
	})

	tabIDPrefix = "proc-a"
	atomic.StoreUint64(&tabIDCounter, 0)
	first := generateTabID()

	tabIDPrefix = "proc-b"
	atomic.StoreUint64(&tabIDCounter, 0)
	second := generateTabID()

	if first == second {
		t.Fatalf("expected distinct tab ids across simulated restart, got %q", first)
	}
}

func TestHandlePtyTabCreated_DoesNotRetargetExistingTabOnSessionReuse(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	reusedSession := tmux.SessionName("amux", wsID, "tab-reused")
	existing := &Tab{
		ID:          TabID("tab-existing"),
		Name:        "claude",
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: reusedSession,
		Running:     true,
	}
	m.tabsByWorkspace[wsID] = []*Tab{existing}

	_ = m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace: ws,
		Assistant: "codex",
		Agent:     &appPty.Agent{Session: reusedSession},
		TabID:     TabID("tab-new"),
		Rows:      24,
		Cols:      80,
		Activate:  true,
	})

	tabs := m.tabsByWorkspace[wsID]
	if len(tabs) != 2 {
		t.Fatalf("expected new tab to be added without mutating existing tab, got %d tabs", len(tabs))
	}
	if tabs[0].Assistant != "claude" {
		t.Fatalf("expected existing tab assistant to remain claude, got %q", tabs[0].Assistant)
	}
	if tabs[1].Assistant != "codex" {
		t.Fatalf("expected new tab assistant codex, got %q", tabs[1].Assistant)
	}
	if tabs[1].ID != TabID("tab-new") {
		t.Fatalf("expected new tab id to be preserved, got %q", tabs[1].ID)
	}
}
