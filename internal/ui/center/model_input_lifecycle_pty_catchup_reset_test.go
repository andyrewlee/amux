package center

import (
	"bytes"
	"testing"
	"time"

	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/vterm"
)

func TestUpdatePTYFlush_StaleCatchUpMessageIgnoredAfterReattachReset(t *testing.T) {
	m := newTestModel()
	m.setTabActorReady()
	m.tabEvents = make(chan tabEvent, 1)

	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                   TabID("tab-reattach-stale-catch-up"),
		Assistant:            "codex",
		Workspace:            ws,
		Terminal:             vterm.New(80, 24),
		catchUpPendingOutput: true,
		pendingOutput:        []byte("old buffered output"),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_, _ = m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Agent:       &appPty.Agent{Session: "sess-reattach-stale-catch-up"},
		Rows:        24,
		Cols:        80,
	})

	payload := bytes.Repeat([]byte("x"), ptyFlushChunkSizeCatchUp+17)
	tab.pendingOutput = append([]byte(nil), payload...)
	tab.lastOutputAt = time.Now().Add(-time.Second)
	tab.flushPendingSince = time.Now().Add(-time.Second)

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID, CatchUp: true})

	select {
	case ev := <-m.tabEvents:
		if ev.kind != tabEventWriteOutput {
			t.Fatalf("expected tabEventWriteOutput, got %v", ev.kind)
		}
		if got, want := len(ev.output), ptyFlushChunkSizeActive; got != want {
			t.Fatalf("queued output len = %d, want normal active cap %d after stale reattach catch-up", got, want)
		}
	default:
		t.Fatalf("expected flush after new output on reattached tab")
	}
}
