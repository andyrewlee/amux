package compositor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestSnapshotDoubleBufferAliasingSafety(t *testing.T) {
	term := vterm.New(8, 4)
	var buf SnapshotDoubleBuffer

	writeAt(term, 1, 0, "old")
	first := buf.Snapshot(term, true)
	if first == nil {
		t.Fatal("first snapshot is nil")
	}
	writeAt(term, 1, 0, "new")
	second := buf.Snapshot(term, true)
	if second == nil {
		t.Fatal("second snapshot is nil")
	}

	if got := rowText(first, 1, 3); got != "old" {
		t.Fatalf("first snapshot row mutated to %q, want old", got)
	}
	if got := rowText(second, 1, 3); got != "new" {
		t.Fatalf("second snapshot row = %q, want new", got)
	}
}

func TestSnapshotDoubleBufferStalenessUnion(t *testing.T) {
	term := vterm.New(10, 8)
	var buf SnapshotDoubleBuffer

	writeAt(term, 3, 0, "row3")
	snapA := buf.Snapshot(term, true)
	requireRowPrefix(t, snapA, 3, "row3")

	writeAt(term, 5, 0, "row5")
	snapB := buf.Snapshot(term, true)
	requireRowPrefix(t, snapB, 3, "row3")
	requireRowPrefix(t, snapB, 5, "row5")

	writeAt(term, 7, 0, "row7")
	snapA2 := buf.Snapshot(term, true)
	requireRowPrefix(t, snapA2, 3, "row3")
	requireRowPrefix(t, snapA2, 5, "row5")
	requireRowPrefix(t, snapA2, 7, "row7")
}

func TestSnapshotDoubleBufferResize(t *testing.T) {
	term := vterm.New(6, 3)
	var buf SnapshotDoubleBuffer

	writeAt(term, 0, 0, "small")
	_ = buf.Snapshot(term, true)

	term.Resize(8, 5)
	writeAt(term, 4, 0, "large")
	resized := buf.Snapshot(term, true)
	if resized == nil {
		t.Fatal("resized snapshot is nil")
	}
	if resized.Width != 8 || resized.Height != 5 {
		t.Fatalf("resized snapshot dims = %dx%d, want 8x5", resized.Width, resized.Height)
	}
	requireRowPrefix(t, resized, 4, "large")

	writeAt(term, 1, 0, "again")
	next := buf.Snapshot(term, true)
	if next.Width != 8 || next.Height != 5 {
		t.Fatalf("next snapshot dims = %dx%d, want 8x5", next.Width, next.Height)
	}
	requireRowPrefix(t, next, 1, "again")
	requireRowPrefix(t, next, 4, "large")
}

func TestSnapshotDoubleBufferViewOffset(t *testing.T) {
	term := vterm.New(8, 3)
	term.Scrollback = [][]vterm.Cell{cellLine("history", 8)}
	term.Screen[0] = cellLine("screen0", 8)
	term.Screen[1] = cellLine("screen1", 8)
	term.Screen[2] = cellLine("screen2", 8)

	var buf SnapshotDoubleBuffer
	term.ViewOffset = 1
	scrolled := buf.Snapshot(term, true)
	requireRowPrefix(t, scrolled, 0, "history")
	requireRowPrefix(t, scrolled, 1, "screen0")

	term.ViewOffset = 0
	live := buf.Snapshot(term, true)
	requireRowPrefix(t, live, 0, "screen0")
	requireRowPrefix(t, live, 2, "screen2")
}

func TestSnapshotDoubleBufferNoChangeFrame(t *testing.T) {
	term := vterm.New(8, 4)
	var buf SnapshotDoubleBuffer

	writeAt(term, 1, 0, "stable")
	_ = buf.Snapshot(term, true)
	_ = buf.Snapshot(term, true)
	snap := buf.Snapshot(term, true)
	if snap == nil {
		t.Fatal("snapshot is nil")
	}
	if got := countDirty(snap.DirtyLines); got > 2 {
		t.Fatalf("dirty lines after no-change frame = %d, want at most cursor rows", got)
	}
	requireRowPrefix(t, snap, 1, "stable")
}

func TestSnapshotDoubleBufferMarkRowStaleReCopiesMutatedRow(t *testing.T) {
	term := vterm.New(8, 4)
	var buf SnapshotDoubleBuffer

	writeAt(term, 2, 0, "clean")
	// Park the cursor off the mutated row so cursor-forced dirty tracking does
	// not re-copy row 2 on its own; MarkRowStale must be the reason it re-copies.
	term.Write([]byte("\x1b[1;1H"))
	first := buf.Snapshot(term, true)
	requireRowPrefix(t, first, 2, "clean")

	// Mutate the handed-out row in place, then record it as stale.
	first.Screen[2][0].Rune = 'X'
	buf.MarkRowStale(2)

	// No vterm change: without MarkRowStale, row 2 would be reused as-is when the
	// double buffer cycles back to buffer 0 and the mutation would survive.
	_ = buf.Snapshot(term, true)
	third := buf.Snapshot(term, true)

	if got := rowText(third, 2, 5); got != "clean" {
		t.Fatalf("stale row not re-copied: row 2 = %q, want clean", got)
	}
}

func writeAt(term *vterm.VTerm, row, col int, text string) {
	term.Write([]byte(fmt.Sprintf("\x1b[%d;%dH%s", row+1, col+1, text)))
}

func requireRowPrefix(t *testing.T, snap *VTermSnapshot, row int, want string) {
	t.Helper()
	if snap == nil {
		t.Fatal("snapshot is nil")
	}
	if got := rowText(snap, row, len(want)); got != want {
		t.Fatalf("row %d prefix = %q, want %q", row, got, want)
	}
}

func rowText(snap *VTermSnapshot, row, width int) string {
	var b strings.Builder
	for i := 0; i < width && row < len(snap.Screen) && i < len(snap.Screen[row]); i++ {
		r := snap.Screen[row][i].Rune
		if r == 0 {
			r = ' '
		}
		b.WriteRune(r)
	}
	return b.String()
}

func cellLine(text string, width int) []vterm.Cell {
	line := vterm.MakeBlankLine(width)
	for i, r := range text {
		if i >= len(line) {
			break
		}
		line[i].Rune = r
		line[i].Width = 1
	}
	return line
}

func countDirty(lines []bool) int {
	count := 0
	for _, dirty := range lines {
		if dirty {
			count++
		}
	}
	return count
}
