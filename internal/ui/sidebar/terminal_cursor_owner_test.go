package sidebar

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/vterm"
)

func setupTerminalOwnerModel(t *testing.T) *TerminalModel {
	t.Helper()
	m := NewTerminalModel()
	ws := &data.Workspace{Repo: "/repo", Root: "/repo/ws"}
	wsID := string(ws.ID())
	m.workspace = ws
	m.focused = true

	ts := &TerminalState{
		VTerm:      vterm.New(10, 3),
		Running:    true,
		lastWidth:  10,
		lastHeight: 3,
	}
	tab := &TerminalTab{
		ID:    generateTerminalTabID(),
		Name:  "Terminal 1",
		State: ts,
	}
	m.tabsByWorkspace[wsID] = []*TerminalTab{tab}
	m.activeTabByWorkspace[wsID] = 0
	return m
}

func TestTerminalLayerWithCursorOwner_HidesCursorWhenNotOwner(t *testing.T) {
	m := setupTerminalOwnerModel(t)

	layer := m.TerminalLayerWithCursorOwner(false)
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if layer.Snap.ShowCursor {
		t.Fatal("expected cursor hidden when sidebar pane does not own cursor")
	}
}

func TestTerminalLayerWithCursorOwner_ShowsCursorWhenOwner(t *testing.T) {
	m := setupTerminalOwnerModel(t)

	layer := m.TerminalLayerWithCursorOwner(true)
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}
	if !layer.Snap.ShowCursor {
		t.Fatal("expected cursor visible when sidebar pane owns cursor")
	}
}

func TestTerminalLayerWithCursorOwner_DoesNotMutatePriorSnapshotOnCacheMiss(t *testing.T) {
	m := setupTerminalOwnerModel(t)
	ts := m.getTerminal()
	if ts == nil || ts.VTerm == nil {
		t.Fatal("expected terminal state")
	}

	ts.mu.Lock()
	ts.VTerm.Write([]byte("a"))
	ts.mu.Unlock()

	layer1 := m.TerminalLayerWithCursorOwner(true)
	if layer1 == nil || layer1.Snap == nil {
		t.Fatal("expected initial terminal layer snapshot")
	}
	if got := layer1.Snap.Screen[0][0].Rune; got != 'a' {
		t.Fatalf("expected initial snapshot rune 'a', got %q", got)
	}

	ts.mu.Lock()
	ts.VTerm.Write([]byte("\rb"))
	ts.mu.Unlock()

	layer2 := m.TerminalLayerWithCursorOwner(true)
	if layer2 == nil || layer2.Snap == nil {
		t.Fatal("expected second terminal layer snapshot")
	}
	if got := layer2.Snap.Screen[0][0].Rune; got != 'b' {
		t.Fatalf("expected second snapshot rune 'b', got %q", got)
	}
	if got := layer1.Snap.Screen[0][0].Rune; got != 'a' {
		t.Fatalf("expected first snapshot to remain unchanged, got %q", got)
	}
	if layer1.Snap == layer2.Snap {
		t.Fatal("expected distinct snapshot objects across cache misses")
	}
}
