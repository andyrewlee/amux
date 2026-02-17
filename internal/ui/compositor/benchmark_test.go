package compositor

import (
	"image"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/andyrewlee/amux/internal/vterm"
)

// Benchmark helper to create a realistic VTerm with content
func setupVTerm(width, height int) *vterm.VTerm {
	term := vterm.New(width, height)
	// Simulate typical terminal content with mixed styles
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			cell := vterm.Cell{
				Rune:  rune('A' + (x+y)%26),
				Width: 1,
				Style: vterm.Style{
					Fg:   vterm.Color{Type: vterm.ColorIndexed, Value: uint32((x + y) % 16)},
					Bold: (x+y)%3 == 0,
				},
			}
			term.Screen[y][x] = cell
		}
	}
	return term
}

// setupVTermSnapshot creates a snapshot for benchmarking
func setupVTermSnapshot(width, height int) *VTermSnapshot {
	term := setupVTerm(width, height)
	return NewVTermSnapshot(term, true)
}

// BenchmarkVTermLayerDrawAt benchmarks the core terminal rendering path
func BenchmarkVTermLayerDrawAt(b *testing.B) {
	sizes := []struct {
		name          string
		width, height int
	}{
		{"80x24", 80, 24},
		{"120x40", 120, 40},
		{"200x60", 200, 60},
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			snap := setupVTermSnapshot(size.width, size.height)
			layer := NewVTermLayer(snap)
			screen := newMockScreen(size.width, size.height)
			rect := image.Rect(0, 0, size.width, size.height)

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				layer.Draw(screen, rect)
			}
		})
	}
}

// BenchmarkVTermSnapshotCreation benchmarks snapshot creation overhead
func BenchmarkVTermSnapshotCreation(b *testing.B) {
	sizes := []struct {
		name          string
		width, height int
	}{
		{"80x24", 80, 24},
		{"120x40", 120, 40},
		{"200x60", 200, 60},
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			term := setupVTerm(size.width, size.height)

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = NewVTermSnapshot(term, true)
			}
		})
	}
}

// BenchmarkVTermSnapshotDirtyReuse benchmarks snapshot creation when only a single line changes.
func BenchmarkVTermSnapshotDirtyReuse(b *testing.B) {
	sizes := []struct {
		name          string
		width, height int
	}{
		{"80x24", 80, 24},
		{"120x40", 120, 40},
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			term := setupVTerm(size.width, size.height)
			snap := NewVTermSnapshotWithCache(term, true, nil)

			moveCursor := []byte("\x1b[2;1H") // row 2, col 1
			writeChar := []byte("X")

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				term.Write(moveCursor)
				term.Write(writeChar)
				snap = NewVTermSnapshotWithCache(term, true, snap)
			}
		})
	}
}

// BenchmarkCanvasRender benchmarks the Canvas.Render ANSI output generation
func BenchmarkCanvasRender(b *testing.B) {
	sizes := []struct {
		name          string
		width, height int
	}{
		{"80x24", 80, 24},
		{"120x40", 120, 40},
		{"200x60", 200, 60},
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			canvas := NewCanvas(size.width, size.height)
			// Fill with mixed content
			for y := 0; y < size.height; y++ {
				for x := 0; x < size.width; x++ {
					canvas.SetCell(x, y, vterm.Cell{
						Rune:  rune('A' + (x+y)%26),
						Width: 1,
						Style: vterm.Style{
							Fg:   vterm.Color{Type: vterm.ColorIndexed, Value: uint32((x + y) % 16)},
							Bold: (x+y)%3 == 0,
						},
					})
				}
			}

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = canvas.Render()
			}
		})
	}
}

// BenchmarkCanvasDrawScreen benchmarks the DrawScreen method
func BenchmarkCanvasDrawScreen(b *testing.B) {
	sizes := []struct {
		name          string
		width, height int
	}{
		{"40x20", 40, 20},   // Small tile
		{"60x30", 60, 30},   // Larger tile
		{"120x40", 120, 40}, // Full size
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			canvas := NewCanvas(size.width, size.height)
			term := setupVTerm(size.width, size.height)
			screen := term.VisibleScreenWithSelection()

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				canvas.DrawScreen(0, 0, size.width, size.height, screen, CursorState{Visible: true}, 0, SelectionRegion{})
			}
		})
	}
}

// BenchmarkStringDrawableDraw benchmarks parsing and drawing ANSI strings
func BenchmarkStringDrawableDraw(b *testing.B) {
	// Create styled content similar to pane chrome
	createStyledContent := func(width, height int) string {
		var sb strings.Builder
		for y := 0; y < height; y++ {
			// Simulate tab bar or border with styles
			sb.WriteString("\x1b[1;34m│\x1b[0m ")
			for x := 2; x < width-2; x++ {
				if y == 0 {
					sb.WriteString("\x1b[1;37m─\x1b[0m")
				} else {
					sb.WriteRune(' ')
				}
			}
			sb.WriteString(" \x1b[1;34m│\x1b[0m")
			if y < height-1 {
				sb.WriteRune('\n')
			}
		}
		return sb.String()
	}

	sizes := []struct {
		name          string
		width, height int
	}{
		{"80x24", 80, 24},
		{"120x40", 120, 40},
		{"200x60", 200, 60},
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			content := createStyledContent(size.width, size.height)
			drawable := NewStringDrawable(content, 0, 0)
			screen := newMockScreen(size.width, size.height)
			rect := image.Rect(0, 0, size.width, size.height)

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				drawable.Draw(screen, rect)
			}
		})
	}
}

// BenchmarkStringDrawableCreation benchmarks the overhead of creating StringDrawables
func BenchmarkStringDrawableCreation(b *testing.B) {
	content := strings.Repeat("\x1b[1;34m│\x1b[0m "+strings.Repeat(" ", 76)+" \x1b[1;34m│\x1b[0m\n", 23)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = NewStringDrawable(content, 0, 0)
	}
}

// BenchmarkStyleToDeltaANSI benchmarks delta style encoding
func BenchmarkStyleToDeltaANSI(b *testing.B) {
	scenarios := []struct {
		name string
		prev vterm.Style
		next vterm.Style
	}{
		{
			name: "no_change",
			prev: vterm.Style{Fg: vterm.Color{Type: vterm.ColorIndexed, Value: 7}},
			next: vterm.Style{Fg: vterm.Color{Type: vterm.ColorIndexed, Value: 7}},
		},
		{
			name: "fg_change",
			prev: vterm.Style{Fg: vterm.Color{Type: vterm.ColorIndexed, Value: 7}},
			next: vterm.Style{Fg: vterm.Color{Type: vterm.ColorIndexed, Value: 1}},
		},
		{
			name: "full_change",
			prev: vterm.Style{
				Fg:   vterm.Color{Type: vterm.ColorIndexed, Value: 7},
				Bold: false,
			},
			next: vterm.Style{
				Fg:   vterm.Color{Type: vterm.ColorRGB, Value: 0xFF0000},
				Bg:   vterm.Color{Type: vterm.ColorRGB, Value: 0x0000FF},
				Bold: true,
			},
		},
		{
			name: "reset_needed",
			prev: vterm.Style{Bold: true, Underline: true},
			next: vterm.Style{},
		},
	}

	for _, s := range scenarios {
		b.Run(s.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = vterm.StyleToDeltaANSI(s.prev, s.next)
			}
		})
	}
}

// BenchmarkStyleToANSI benchmarks full style encoding
func BenchmarkStyleToANSI(b *testing.B) {
	scenarios := []struct {
		name  string
		style vterm.Style
	}{
		{
			name:  "default",
			style: vterm.Style{},
		},
		{
			name:  "indexed_fg",
			style: vterm.Style{Fg: vterm.Color{Type: vterm.ColorIndexed, Value: 7}},
		},
		{
			name: "rgb_full",
			style: vterm.Style{
				Fg:        vterm.Color{Type: vterm.ColorRGB, Value: 0xFF0000},
				Bg:        vterm.Color{Type: vterm.ColorRGB, Value: 0x0000FF},
				Bold:      true,
				Underline: true,
			},
		},
	}

	for _, s := range scenarios {
		b.Run(s.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = vterm.StyleToANSI(s.style)
			}
		})
	}
}

// Mock implementations for benchmarking that implement uv.Screen

type mockScreen struct {
	width, height int
}

func newMockScreen(width, height int) *mockScreen {
	return &mockScreen{width: width, height: height}
}

func (s *mockScreen) Bounds() image.Rectangle {
	return image.Rect(0, 0, s.width, s.height)
}

func (s *mockScreen) CellAt(x, y int) *uv.Cell {
	return nil
}

func (s *mockScreen) SetCell(x, y int, c *uv.Cell) {
	// No-op for benchmarking - we just want to measure the layer's work
}

type wcWidth struct{}

func (wcWidth) StringWidth(s string) int { return len(s) }

func (s *mockScreen) WidthMethod() uv.WidthMethod {
	return wcWidth{}
}

// BenchmarkChromeCacheHit benchmarks the cache hit path
func BenchmarkChromeCacheHit(b *testing.B) {
	content := strings.Repeat("\x1b[1;34m│\x1b[0m "+strings.Repeat(" ", 76)+" \x1b[1;34m│\x1b[0m\n", 23)
	cache := &ChromeCache{}

	// Prime the cache
	drawable := NewStringDrawable(content, 0, 0)
	cache.Set(content, 80, 24, true, 0, 0, drawable)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = cache.Get(content, 80, 24, true, 0, 0)
	}
}

// BenchmarkChromeCacheMiss benchmarks the cache miss + rebuild path
func BenchmarkChromeCacheMiss(b *testing.B) {
	content := strings.Repeat("\x1b[1;34m│\x1b[0m "+strings.Repeat(" ", 76)+" \x1b[1;34m│\x1b[0m\n", 23)
	cache := &ChromeCache{}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cache.Invalidate()
		if cached := cache.Get(content, 80, 24, true, 0, 0); cached == nil {
			drawable := NewStringDrawable(content, 0, 0)
			cache.Set(content, 80, 24, true, 0, 0, drawable)
		}
	}
}

// BenchmarkFastHash benchmarks the hash function
func BenchmarkFastHash(b *testing.B) {
	content := strings.Repeat("\x1b[1;34m│\x1b[0m "+strings.Repeat(" ", 76)+" \x1b[1;34m│\x1b[0m\n", 23)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = FastHash(content)
	}
}

// BenchmarkSnapshotCacheHit simulates cache hit by reusing a snapshot
func BenchmarkSnapshotCacheHit(b *testing.B) {
	sizes := []struct {
		name          string
		width, height int
	}{
		{"80x24", 80, 24},
		{"120x40", 120, 40},
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			snap := setupVTermSnapshot(size.width, size.height)

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				// Simulates cache hit - just create the layer wrapper
				_ = NewVTermLayer(snap)
			}
		})
	}
}

// BenchmarkSnapshotCacheMiss benchmarks the full snapshot creation path
func BenchmarkSnapshotCacheMiss(b *testing.B) {
	sizes := []struct {
		name          string
		width, height int
	}{
		{"80x24", 80, 24},
		{"120x40", 120, 40},
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			term := setupVTerm(size.width, size.height)

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				// Full snapshot creation
				snap := NewVTermSnapshot(term, true)
				_ = NewVTermLayer(snap)
			}
		})
	}
}
