package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestUpdatePTYOutput_DoesNotExtendBootstrapOnEachChunk(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	bootstrapAt := time.Now().Add(-500 * time.Millisecond)
	tab := &Tab{
		ID:                    TabID("tab-1"),
		Assistant:             "codex",
		Workspace:             ws,
		SessionName:           "amux-test-session",
		Terminal:              vterm.New(80, 24),
		Running:               true,
		bootstrapActivity:     true,
		bootstrapLastOutputAt: bootstrapAt,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("streaming output"),
	})

	tab.mu.Lock()
	bootstrap := tab.bootstrapActivity
	after := tab.bootstrapLastOutputAt
	tab.mu.Unlock()
	if !bootstrap {
		t.Fatal("expected bootstrapActivity to remain active before quiet-gap timeout")
	}
	if !after.Equal(bootstrapAt) {
		t.Fatalf("expected bootstrapLastOutputAt unchanged while bootstrap active, before=%v after=%v", bootstrapAt, after)
	}
}
