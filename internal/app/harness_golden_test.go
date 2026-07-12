package app

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// updateGolden regenerates the checked-in golden frames instead of asserting
// against them. Run `go test ./internal/app -run Golden -update` after an
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
		// Overlay presets. Adding/altering a dialog or overlay is the most
		// common UI change an agent makes, so each of these snapshots a distinct
		// composeOverlays path (confirm dialog, settings, prefix palette, error
		// overlay, input dialog) over the same center base pane. Only
		// deterministic, filesystem-independent overlays are golden-able; the file
		// picker (reads the real filesystem) and the toast (wall-clock visibility)
		// are deliberately omitted.
		{
			name: "overlay_dialog",
			opts: HarnessOptions{
				Mode:    HarnessCenter,
				Tabs:    8,
				Width:   width,
				Height:  height,
				Overlay: HarnessOverlayDialog,
			},
			steps: steps,
		},
		{
			name: "overlay_settings",
			opts: HarnessOptions{
				Mode:    HarnessCenter,
				Tabs:    8,
				Width:   width,
				Height:  height,
				Overlay: HarnessOverlaySettings,
			},
			steps: steps,
		},
		{
			name: "overlay_prefix",
			opts: HarnessOptions{
				Mode:    HarnessCenter,
				Tabs:    8,
				Width:   width,
				Height:  height,
				Overlay: HarnessOverlayPrefix,
			},
			steps: steps,
		},
		{
			name: "overlay_error",
			opts: HarnessOptions{
				Mode:    HarnessCenter,
				Tabs:    8,
				Width:   width,
				Height:  height,
				Overlay: HarnessOverlayError,
			},
			steps: steps,
		},
		{
			name: "overlay_input",
			opts: HarnessOptions{
				Mode:    HarnessCenter,
				Tabs:    8,
				Width:   width,
				Height:  height,
				Overlay: HarnessOverlayInput,
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
					"got %d bytes, want %d bytes\n%s",
					p.name, path, len(got), len(want), goldenLineDiff(got, string(want)))
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

// goldenLineDiff renders a localized, human-readable diff between a rendered
// frame and its golden so a failing agent can see WHERE the frame diverged
// (which row, border cell, or SGR/color run) instead of only byte counts. It
// splits both on "\n", finds the first differing line, and quotes the got/want
// for that line plus a couple of leading context lines, escaping non-printing
// bytes (ANSI escapes, control chars) via strconv.Quote so the runs are legible
// in plain test output. It is dependency-free and intentionally cheap.
func goldenLineDiff(got, want string) string {
	gotLines := strings.Split(got, "\n")
	wantLines := strings.Split(want, "\n")

	first := firstDiffLine(gotLines, wantLines)
	if first < 0 {
		// Contents are equal line-by-line; the byte difference is a trailing
		// newline or an extra/missing final line. Report the line-count delta.
		return fmt.Sprintf("first diff: line count differs (got %d lines, want %d lines)",
			len(gotLines), len(wantLines))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "first diff at line %d (1-based):\n", first+1)
	// A small leading window helps locate the divergence in a busy frame.
	const context = 2
	start := first - context
	if start < 0 {
		start = 0
	}
	for i := start; i < first; i++ {
		fmt.Fprintf(&b, "  line %d: %s\n", i+1, strconv.Quote(lineAt(gotLines, i)))
	}
	fmt.Fprintf(&b, "  line %d got:  %s\n", first+1, strconv.Quote(lineAt(gotLines, first)))
	fmt.Fprintf(&b, "  line %d want: %s", first+1, strconv.Quote(lineAt(wantLines, first)))
	return b.String()
}

// firstDiffLine returns the index of the first line that differs between got
// and want, or -1 when every overlapping line matches (lengths may still
// differ, which the caller reports separately).
func firstDiffLine(got, want []string) int {
	n := len(got)
	if len(want) < n {
		n = len(want)
	}
	for i := 0; i < n; i++ {
		if got[i] != want[i] {
			return i
		}
	}
	// Every overlapping line matched; any remaining byte difference is a
	// line-count delta, which the caller reports.
	return -1
}

// lineAt returns lines[i] or a sentinel when i is out of range, so the diff can
// show that one side ran out of lines without panicking.
func lineAt(lines []string, i int) string {
	if i < 0 || i >= len(lines) {
		return "<no such line>"
	}
	return lines[i]
}

func TestGoldenLineDiff(t *testing.T) {
	t.Run("reports first differing line with quoted got/want", func(t *testing.T) {
		got := "row0\nrow1\x1b[31mRED\nrow2"
		want := "row0\nrow1\x1b[32mGREEN\nrow2"
		out := goldenLineDiff(got, want)
		if !strings.Contains(out, "first diff at line 2") {
			t.Fatalf("expected diff to localize line 2, got:\n%s", out)
		}
		// The ANSI escape must be escaped (no raw ESC byte) so it is legible.
		if strings.ContainsRune(out, '\x1b') {
			t.Fatalf("expected escape byte to be quoted, got raw ESC in:\n%s", out)
		}
		if !strings.Contains(out, `\x1b[31mRED`) || !strings.Contains(out, `\x1b[32mGREEN`) {
			t.Fatalf("expected quoted got/want SGR runs, got:\n%s", out)
		}
	})

	t.Run("reports line-count delta when overlapping lines match", func(t *testing.T) {
		got := "a\nb\nc"
		want := "a\nb"
		out := goldenLineDiff(got, want)
		if !strings.Contains(out, "line count differs") {
			t.Fatalf("expected line-count delta report, got:\n%s", out)
		}
	})
}

func assertDistinctFrames(t *testing.T, frames map[string]string) {
	t.Helper()
	// Every preset (base panes and overlays) must render a distinct frame, or
	// its golden would not actually distinguish one render path from another.
	names := make([]string, 0, len(frames))
	for _, p := range goldenPresets() {
		if _, ok := frames[p.name]; ok {
			names = append(names, p.name)
		}
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if frames[names[i]] == frames[names[j]] {
				t.Errorf("presets %q and %q rendered byte-identical frames; pick args that diverge so each golden guards a distinct path",
					names[i], names[j])
			}
		}
	}
}
