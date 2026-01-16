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
