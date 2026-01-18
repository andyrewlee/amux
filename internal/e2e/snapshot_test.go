package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/app"
)

const (
	monitorSnap = "monitor.snap"
	centerSnap  = "center.snap"
)

func TestMonitorSnapshot(t *testing.T) {
	opts := app.HarnessOptions{
		Mode:         app.HarnessMonitor,
		Tabs:         4,
		Width:        80,
		Height:       24,
		HotTabs:      1,
		PayloadBytes: 32,
	}
	h, err := app.NewHarness(opts)
	if err != nil {
		t.Fatalf("harness init: %v", err)
	}
	for i := 0; i < 3; i++ {
		h.Step(i)
	}
	view := h.Render()
	got := BufferToASCII(RenderViewToBuffer(view, opts.Width, opts.Height))
	assertSnapshot(t, monitorSnap, got)
}

func TestCenterSnapshot(t *testing.T) {
	opts := app.HarnessOptions{
		Mode:         app.HarnessCenter,
		Tabs:         2,
		Width:        120,
		Height:       24,
		HotTabs:      1,
		PayloadBytes: 32,
	}
	h, err := app.NewHarness(opts)
	if err != nil {
		t.Fatalf("harness init: %v", err)
	}
	for i := 0; i < 3; i++ {
		h.Step(i)
	}
	view := h.Render()
	got := BufferToASCII(RenderViewToBuffer(view, opts.Width, opts.Height))
	assertSnapshot(t, centerSnap, got)
}

func assertSnapshot(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("AMUX_UPDATE_SNAPSHOTS") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write snapshot: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if strings.TrimRight(string(want), "\n") != strings.TrimRight(got, "\n") {
		t.Fatalf("snapshot mismatch for %s (set AMUX_UPDATE_SNAPSHOTS=1 to update)", name)
	}
}
