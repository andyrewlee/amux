package compositor

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestVTermLayerSelectionCursorOverlap(t *testing.T) {
	term := vterm.New(3, 1)
	term.CursorX = 0
	term.CursorY = 0
	term.SetSelection(0, 0, 0, 0, true)

	snap := NewVTermSnapshot(term, true)
	if snap == nil {
		t.Fatalf("expected snapshot, got nil")
	}

	cell := snap.Screen[0][0]
	uvCell := cellToUVSnapshot(cell, snap, 0, 0)
	defer putCell(uvCell)

	if uvCell.Style.Attrs&uv.AttrReverse == 0 {
		t.Fatalf("expected reverse attribute for selection+cursor overlap")
	}
}

type bufferScreen struct {
	*uv.Buffer
}

type testWidth struct{}

func (testWidth) StringWidth(s string) int { return len(s) }

func (s *bufferScreen) WidthMethod() uv.WidthMethod {
	return testWidth{}
}

func TestVTermLayerClearsContinuationCells(t *testing.T) {
	term := vterm.New(2, 1)
	term.Screen[0][0] = vterm.Cell{Rune: '中', Width: 2}
	term.Screen[0][1] = vterm.Cell{Width: 0}

	snap := NewVTermSnapshot(term, true)
	if snap == nil {
		t.Fatalf("expected snapshot, got nil")
	}
	layer := NewVTermLayer(snap)

	screen := &bufferScreen{Buffer: uv.NewBuffer(2, 1)}
	// Seed stale content in the continuation cell.
	screen.SetCell(1, 0, &uv.Cell{Content: "X", Width: 1})
	layer.Draw(screen, screen.Bounds())

	cell := screen.CellAt(1, 0)
	if cell == nil {
		t.Fatalf("expected cell to be written at continuation position")
	}
	if cell.Width != 0 || cell.Content != "" {
		t.Fatalf("expected continuation cell to be cleared, got width=%d content=%q", cell.Width, cell.Content)
	}
}

func TestVTermSnapshotRespectsViewOffsetChange(t *testing.T) {
	term := vterm.New(2, 1)
	live := vterm.MakeBlankLine(2)
	live[0] = vterm.Cell{Rune: 'A', Width: 1}
	term.Screen[0] = live

	scroll := vterm.MakeBlankLine(2)
	scroll[0] = vterm.Cell{Rune: 'B', Width: 1}
	term.Scrollback = [][]vterm.Cell{scroll}

	term.ViewOffset = 1
	snap := NewVTermSnapshotWithCache(term, true, nil)
	if snap == nil {
		t.Fatalf("expected snapshot, got nil")
	}
	if snap.Screen[0][0].Rune != 'B' {
		t.Fatalf("expected scrollback cell, got %q", snap.Screen[0][0].Rune)
	}

	term.ViewOffset = 0
	snap2 := NewVTermSnapshotWithCache(term, true, snap)
	if snap2 == nil {
		t.Fatalf("expected snapshot, got nil")
	}
	if snap2.Screen[0][0].Rune != 'A' {
		t.Fatalf("expected live cell after ViewOffset reset, got %q", snap2.Screen[0][0].Rune)
	}
}
