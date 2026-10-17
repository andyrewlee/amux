package app

import (
	"testing"
)

// Benchmarks for plan 117's composition-cost question: what does a frame of
// each harness surface cost — whole-frame viewLayerBased including the vterm
// write parse, snapshot, gates, and canvas compose — measured headlessly so
// the numbers isolate composition rather than host scheduling noise. The
// sub-benchmarks mirror the make perf-check presets so results are comparable
// to the checked-in p95s.

func benchHarnessCompose(b *testing.B, opts HarnessOptions) {
	b.Helper()
	h, err := NewHarness(opts)
	if err != nil {
		b.Fatalf("NewHarness: %v", err)
	}
	// Warm the frame cache/gates/snapshot buffers so the measured frames are
	// the steady-state streaming case, not first-render setup.
	for i := 0; i < 30; i++ {
		h.Step(i)
		_ = h.Render()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Step(i)
		_ = h.Render()
	}
}

func BenchmarkComposeCenter(b *testing.B) {
	benchHarnessCompose(b, HarnessOptions{
		Mode: HarnessCenter, Tabs: 16, HotTabs: 2,
		PayloadBytes: 64, Width: 160, Height: 48,
	})
}

func BenchmarkComposeSidebar(b *testing.B) {
	benchHarnessCompose(b, HarnessOptions{
		Mode: HarnessSidebar, Tabs: 16, HotTabs: 1,
		PayloadBytes: 64, NewlineEvery: 1, Width: 160, Height: 48,
	})
}

func BenchmarkComposeMonitor(b *testing.B) {
	benchHarnessCompose(b, HarnessOptions{
		Mode: HarnessMonitor, Tabs: 16, HotTabs: 4,
		PayloadBytes: 64, Width: 160, Height: 48,
	})
}
