package center

import (
	"reflect"
	"testing"

	"github.com/andyrewlee/amux/internal/vterm"
)

// TestTerminalLayerReusesChatSnapshotRows proves chat tabs now share the
// snapshot double buffer: an unchanged row is re-used (same backing array)
// across frames instead of being reallocated every frame. Before chat tabs were
// flipped onto the buffer, each frame allocated a fresh snapshot and the row
// pointers always differed.
func TestTerminalLayerChatReuseSharesRowBackingArray(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(20, 12)
	// Row 2 is written once and never touched again; keep the cursor far away on
	// row 8 so cursor-row dirty tracking and chat cursor sanitization never mark
	// row 2 stale.
	term.Write([]byte("\x1b[3;1Hkeepme"))
	term.Write([]byte("\x1b[9;1H"))

	m.tabs.ByWorkspace[wsID] = []*Tab{
		{
			ID:        TabID("tab-chat-reuse"),
			Assistant: "codex",
			Workspace: ws,
			Terminal:  term,
		},
	}
	m.tabs.ActiveByWorkspace[wsID] = 0
	m.SetWorkspace(ws)
	m.Focus()

	const keepRow = 2

	first := m.TerminalLayerWithCursorOwner(true)
	if first == nil || first.Snap == nil {
		t.Fatal("expected first terminal layer snapshot")
	}
	rowA := first.Snap.Screen[keepRow]

	// Two more frames, each dirtying only row 8 and bumping the version so the
	// snapshot cache misses and the double buffer alternates: frame 1 and frame 3
	// land in the same buffer, frame 2 in the other.
	term.Write([]byte("\x1b[9;1Habc"))
	if second := m.TerminalLayerWithCursorOwner(true); second == nil || second.Snap == nil {
		t.Fatal("expected second terminal layer snapshot")
	}
	term.Write([]byte("\x1b[9;1Hdef"))
	third := m.TerminalLayerWithCursorOwner(true)
	if third == nil || third.Snap == nil {
		t.Fatal("expected third terminal layer snapshot")
	}
	rowB := third.Snap.Screen[keepRow]

	if reflect.ValueOf(rowA).Pointer() != reflect.ValueOf(rowB).Pointer() {
		t.Fatal("expected unchanged chat row to reuse its backing array across frames")
	}
}
