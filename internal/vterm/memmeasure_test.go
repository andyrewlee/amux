package vterm

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

// Plan-010 spike measurements: how much memory a saturated scrollback actually
// costs. Run with:
//
//	go test ./internal/vterm/ -run TestCellMemoryProfile -v
//	go test ./internal/vterm/ -bench BenchmarkScrollbackAlloc -benchmem
//
// These tests are cheap (a few hundred ms) and are kept so the numbers behind
// the allocation recommendation stay reproducible.

func heapInUse() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapInuse
}

func TestCellMemoryProfile(t *testing.T) {
	t.Logf("sizeof: Cell=%d Style=%d Color=%d (Rune+Width+GraphemeCluster=%d)",
		unsafe.Sizeof(Cell{}), unsafe.Sizeof(Style{}), unsafe.Sizeof(Color{}),
		unsafe.Sizeof(rune(0))+unsafe.Sizeof(0)+unsafe.Sizeof(""))

	const rows, cols = MaxScrollback, 160

	before := heapInUse()
	blank := make([][]Cell, 0, rows)
	for i := 0; i < rows; i++ {
		blank = append(blank, MakeBlankLine(cols))
	}
	blankBytes := heapInUse() - before
	runtime.KeepAlive(blank)
	t.Logf("blank scrollback %dx%d: %.1f MB (%.0f B/row)", rows, cols,
		float64(blankBytes)/1e6, float64(blankBytes)/rows)

	// Realistic short-line content: scrollback rows are stored at terminal
	// width regardless of content (vterm.go appends v.Screen[0] verbatim), so
	// a 40-column ls line still costs a full-width row. This variant writes
	// text into a full-width row to prove the point.
	before = heapInUse()
	realistic := make([][]Cell, 0, rows)
	for i := 0; i < rows; i++ {
		row := MakeBlankLine(cols)
		for x := 0; x < 40; x++ {
			row[x] = Cell{Rune: 'x', Width: 1}
		}
		realistic = append(realistic, row)
	}
	realBytes := heapInUse() - before
	runtime.KeepAlive(realistic)
	t.Logf("full-width rows w/ 40 cells of text %dx%d: %.1f MB", rows, cols, float64(realBytes)/1e6)

	// Hypothetical trimmed-tail representation: rows stored at used width.
	before = heapInUse()
	trimmed := make([][]Cell, 0, rows)
	for i := 0; i < rows; i++ {
		row := make([]Cell, 40)
		for x := range row {
			row[x] = Cell{Rune: 'x', Width: 1}
		}
		trimmed = append(trimmed, row)
	}
	trimBytes := heapInUse() - before
	runtime.KeepAlive(trimmed)
	t.Logf("trimmed-width rows (40 cells) %dx40: %.1f MB", rows, float64(trimBytes)/1e6)
}

// TestSaturatedVTermRSS drives the real parser with a large stream and reports
// the vterm's heap footprint — the number that actually matters per tab.
func TestSaturatedVTermRSS(t *testing.T) {
	const cols = 160
	vt := New(cols, 48)

	// Synthesize ~12k lines of mixed content: a 40-char printable body with a
	// colored SGR prefix, then CRLF — resembling agent output under load.
	var b strings.Builder
	for i := 0; i < 12000; i++ {
		fmt.Fprintf(&b, "\x1b[32m%04d worker ok — done\x1b[0m\r\n", i)
	}
	data := []byte(b.String())

	before := heapInUse()
	for off := 0; off < len(data); off += 1 << 16 {
		end := off + 1<<16
		if end > len(data) {
			end = len(data)
		}
		vt.Write(data[off:end])
	}
	after := heapInUse()
	t.Logf("saturated vterm (%d lines fed, scrollback=%d rows): heap delta %.1f MB",
		12000, len(vt.Scrollback), float64(after-before)/1e6)
}

func BenchmarkScrollbackAlloc(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rows := make([][]Cell, 0, 1000)
		for r := 0; r < 1000; r++ {
			rows = append(rows, MakeBlankLine(160))
		}
		runtime.KeepAlive(rows)
	}
}
