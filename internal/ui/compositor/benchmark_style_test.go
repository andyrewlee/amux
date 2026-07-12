package compositor

import (
	"image"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/andyrewlee/amux/internal/vterm"
)

func BenchmarkStyleDeltaANSI(b *testing.B) {
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
				_ = s.prev.DeltaANSI(s.next)
			}
		})
	}
}

// BenchmarkStyleANSI benchmarks full style encoding
func BenchmarkStyleANSI(b *testing.B) {
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
				_ = s.style.ANSI()
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
