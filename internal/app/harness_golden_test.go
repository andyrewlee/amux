package app

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updateGolden regenerates the checked-in golden frames instead of asserting
// against them. Run `go test ./internal/app/... -run Golden -update` after an
// intentional render change, then commit the refreshed testdata.
var updateGolden = flag.Bool("update", false, "update golden frame files")

// goldenPreset describes a deterministic harness configuration whose final
// rendered frame is byte-stable, so it can be snapshotted as a golden.
//
// The render pipeline (NewHarness -> Step/Render -> App.viewLayerBased) and
// buildPayload are fully deterministic for fixed options, so the final
// view.Content ANSI frame is reproducible. Unlike -assert-min-visible (which
// only counts visible glyphs), a byte-exact golden also catches a broken
// border, wrong color/SGR, an off-by-one, or a truncation regression.
//
// NOTE: the monitor harness reuses the sidebar harness (monitor differs only in
// h.mode), and in this render path the streamed terminal output is composited
// into the center pane but NOT the sidebar/monitor bottom pane, so varying
// hot-tabs / newline-every / tabs alone leaves sidebar and monitor frames
// byte-identical. To keep each golden guarding a distinct path we diverge on a
// rendering-affecting input instead: center streams visible output into the
// center pane, sidebar renders without keymap hints, and monitor renders with
// keymap hints enabled. The test asserts the three goldens are mutually
// distinct to keep that honest.
//
// CAVEAT for future maintainers: the sidebar and monitor presets still pass
// streaming args (HotTabs, PayloadBytes, NewlineEvery), and Step does write that
// payload into the sidebar vterm, but in these modes that streamed output is NOT
// composited into the rendered frame — so the sidebar/monitor goldens guard only
// the static chrome (borders, dashboard list, the keymap-hint row), and the
// streaming args they pass are exercised at the vterm-write level but do not
// appear in the snapshotted bytes. The center preset is the SOLE golden that
// guards live-stream vterm composition. Do not assume tweaking the sidebar or
// monitor streaming args will change their golden frames; only a chrome or
// keymap-hint change will. If you want a sidebar/monitor streaming-composition
// guard, wire the streamed pane into those modes' render path first, then
// regenerate.
type goldenPreset struct {
	name  string
	opts  HarnessOptions
	steps int
}

func goldenPresets() []goldenPreset {
	// Modest, fixed geometry keeps goldens small and reviewable.
	const (
		width  = 120
		height = 36
		steps  = 24
	)
	return []goldenPreset{
		{
			name: "center",
			opts: HarnessOptions{
				Mode:         HarnessCenter,
				Tabs:         8,
				Width:        width,
				Height:       height,
				HotTabs:      2,
				PayloadBytes: 64,
				NewlineEvery: 4,
			},
			steps: steps,
		},
		{
			name: "sidebar",
			opts: HarnessOptions{
				Mode:         HarnessSidebar,
				Tabs:         8,
				Width:        width,
				Height:       height,
				HotTabs:      1,
				PayloadBytes: 64,
				NewlineEvery: 1,
			},
			steps: steps,
		},
		{
			name: "monitor",
			opts: HarnessOptions{
				Mode:            HarnessMonitor,
				Tabs:            8,
				Width:           width,
				Height:          height,
				HotTabs:         3,
				PayloadBytes:    48,
				NewlineEvery:    0,
				ShowKeymapHints: true,
			},
			steps: steps,
		},
	}
}

// renderGoldenFrame builds the harness for a preset and drives it to its final
// frame, returning the exact view.Content bytes a headless agent would see.
func renderGoldenFrame(t *testing.T, p goldenPreset) string {
	t.Helper()
	h, err := NewHarness(p.opts)
	if err != nil {
		t.Fatalf("%s: harness init: %v", p.name, err)
	}
	var content string
	for i := 0; i < p.steps; i++ {
		h.Step(i)
		content = h.Render().Content
	}
	if content == "" {
		t.Fatalf("%s: final frame is empty", p.name)
	}
	return content
}

func goldenPath(name string) string {
	return filepath.Join("testdata", "golden", name+".frame")
}

func TestHarnessGoldenFrames(t *testing.T) {
	frames := make(map[string]string)
	for _, p := range goldenPresets() {
		t.Run(p.name, func(t *testing.T) {
			got := renderGoldenFrame(t, p)
			frames[p.name] = got
			path := goldenPath(p.name)

			if *updateGolden {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("%s: mkdir golden dir: %v", p.name, err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatalf("%s: write golden: %v", p.name, err)
				}
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%s: read golden (run `go test ./internal/app/... -run Golden -update` to create): %v", p.name, err)
			}
			if got != string(want) {
				t.Errorf("%s: rendered frame does not match golden %s.\n"+
					"If this change is intentional, regenerate with:\n"+
					"  go test ./internal/app/... -run Golden -update\n"+
					"got %d bytes, want %d bytes", p.name, path, len(got), len(want))
			}
		})
	}

	// Guard the caveat: the three presets must produce distinct frames, or the
	// goldens would not actually distinguish a sidebar regression from a
	// monitor one.
	if !*updateGolden {
		assertDistinctFrames(t, frames)
	}
}

func assertDistinctFrames(t *testing.T, frames map[string]string) {
	t.Helper()
	names := []string{"center", "sidebar", "monitor"}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			a, okA := frames[names[i]]
			b, okB := frames[names[j]]
			if !okA || !okB {
				continue
			}
			if a == b {
				t.Errorf("presets %q and %q rendered byte-identical frames; pick args that diverge so each golden guards a distinct path",
					names[i], names[j])
			}
		}
	}
}
