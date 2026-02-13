package center

import (
	"bytes"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestUpdatePTYFlush_UsesLargerChunkForActiveTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                TabID("tab-active"),
		Workspace:         ws,
		Terminal:          vterm.New(80, 24),
		Running:           true,
		lastOutputAt:      time.Now().Add(-time.Second),
		flushPendingSince: time.Now().Add(-time.Second),
		pendingOutput:     bytes.Repeat([]byte("x"), ptyFlushChunkSizeActive+17),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})

	if got, want := len(tab.pendingOutput), 17; got != want {
		t.Fatalf("pending output = %d, want %d", got, want)
	}
}

func TestUpdatePTYFlush_UsesBaseChunkForInactiveTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	active := &Tab{
		ID:        TabID("tab-active"),
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
		Running:   true,
	}
	inactive := &Tab{
		ID:                TabID("tab-inactive"),
		Workspace:         ws,
		Terminal:          vterm.New(80, 24),
		Running:           true,
		lastOutputAt:      time.Now().Add(-time.Second),
		flushPendingSince: time.Now().Add(-time.Second),
		pendingOutput:     bytes.Repeat([]byte("x"), ptyFlushChunkSize+17),
	}
	m.tabsByWorkspace[wsID] = []*Tab{active, inactive}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: inactive.ID})

	if got, want := len(inactive.pendingOutput), 17; got != want {
		t.Fatalf("pending output = %d, want %d", got, want)
	}
}
