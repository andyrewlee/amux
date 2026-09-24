package compositor

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestVTermSnapshotPendingWrapCursorRenders(t *testing.T) {
	term := vterm.New(10, 3)
	term.Write([]byte("aaaaaaaaaa")) // fill row 0 to the last column
	if !term.PendingWrap {
		t.Fatal("fixture did not produce pending-wrap state")
	}

	snap := NewVTermSnapshot(term, true)
	if !snap.CursorPendingWrap {
		t.Fatal("snapshot did not carry CursorPendingWrap")
	}
	if got := snap.CursorRenderX(); got != snap.Width-1 {
		t.Fatalf("CursorRenderX() = %d, want %d (last column)", got, snap.Width-1)
	}

	// The last-column cell must carry the cursor's reverse attribute.
	cell := snap.Screen[0][snap.Width-1]
	var uvCell uv.Cell
	cellToUVSnapshot(&uvCell, cell, snap, snap.Width-1, 0, false)
	if uvCell.Style.Attrs&uv.AttrReverse == 0 {
		t.Fatal("cursor cell at last column lacks reverse attribute — cursor invisible during pending wrap")
	}
}
