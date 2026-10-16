package vterm

import (
	"fmt"
	"testing"
)

// BenchmarkSyncFreeze measures the per-sync-window snapshot cost. tmux
// brackets every client redraw in DEC 2026, so this runs once per rendered
// frame on a tmux-backed tab; the copy dominates GC pressure on the render
// path.
func BenchmarkSyncFreeze(b *testing.B) {
	for _, dims := range [][2]int{{80, 24}, {200, 50}} {
		b.Run(fmt.Sprintf("%dx%d", dims[0], dims[1]), func(b *testing.B) {
			v := New(dims[0], dims[1])
			// One begin/end before the timer primes the pooled frame so the
			// loop measures steady-state cost.
			v.setSynchronizedOutput(true)
			v.setSynchronizedOutput(false)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				v.setSynchronizedOutput(true)
				v.setSynchronizedOutput(false)
			}
		})
	}
}

// BenchmarkVisibleCaptureFrame measures the capture allocation. The common
// shape is a mostly-blank screen with content in the tail rows — an alloc
// bounded by retained rows should dwarf the full-screen copy.
func BenchmarkVisibleCaptureFrame(b *testing.B) {
	for _, dims := range [][2]int{{80, 24}, {200, 50}} {
		b.Run(fmt.Sprintf("%dx%d tail content", dims[0], dims[1]), func(b *testing.B) {
			v := New(dims[0], dims[1])
			v.Write([]byte("\x1b[2J"))
			v.Write([]byte(fmt.Sprintf("\x1b[%d;1H", dims[1]-3)))
			v.Write([]byte("$ make dev\r\nbuilding…\r\n$ "))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = v.visibleCaptureFrame()
			}
		})
		b.Run(fmt.Sprintf("%dx%d full screen", dims[0], dims[1]), func(b *testing.B) {
			v := New(dims[0], dims[1])
			v.Write([]byte("\x1b[2J"))
			v.Write([]byte("\x1b[1;1H"))
			for y := 0; y < dims[1]-1; y++ {
				v.Write([]byte("content row\r\n"))
			}
			v.Write([]byte("last"))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = v.visibleCaptureFrame()
			}
		})
	}
}
