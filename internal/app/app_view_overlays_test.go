package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/perf"
)

// TestRenderErrorOverlay_NilErrorReturnsEmpty verifies the overlay is empty when
// no error is present: with a.err == nil the function must short-circuit to ""
// rather than render an empty bordered box.
func TestRenderErrorOverlay_NilErrorReturnsEmpty(t *testing.T) {
	a := &App{}

	if got := a.renderErrorOverlay(); got != "" {
		t.Fatalf("expected empty overlay when a.err is nil, got: %q", got)
	}
}

// TestRenderErrorOverlay_RendersErrorTextAndHint covers the populated-error path
// across a range of message shapes. The overlay must surface the concrete error
// text behind the "Error: " prefix and always carry the dismiss hint. The fixed
// 56-column style word-wraps long messages, so we assert on the de-styled,
// whitespace-normalized text rather than the raw bytes — that proves the message
// content survives rendering without coupling the test to wrap positions.
func TestRenderErrorOverlay_RendersErrorTextAndHint(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  string
	}{
		{name: "short message", msg: "boom"},
		{name: "message with spaces", msg: "tmux session not found"},
		{name: "message with colon", msg: "git: fatal: not a repository"},
		{name: "long wrapping message", msg: strings.Repeat("overflow ", 40)},
		{name: "empty message", msg: ""},
		{name: "unicode message", msg: "файл не найден"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &App{err: errors.New(tc.msg)}

			got := a.renderErrorOverlay()

			if got == "" {
				t.Fatal("expected a non-empty overlay when a.err is set")
			}
			plain := normalizeOverlayText(got)
			wantErr := strings.TrimSpace("Error: " + tc.msg)
			if !strings.Contains(plain, wantErr) {
				t.Fatalf("expected overlay to contain the error text %q, got de-styled: %q", wantErr, plain)
			}
			if !strings.Contains(plain, "Press any key to dismiss.") {
				t.Fatalf("expected overlay to contain the dismiss hint, got de-styled: %q", plain)
			}
		})
	}
}

// normalizeOverlayText strips ANSI styling and collapses border/wrap whitespace
// so an overlay's logical text can be substring-matched independent of the box
// chrome and word-wrap positions introduced by the fixed-width render.
func normalizeOverlayText(view string) string {
	plain := ansi.Strip(view)
	plain = strings.Map(func(r rune) rune {
		switch r {
		case '│', '╭', '╮', '╰', '╯', '─':
			return ' '
		default:
			return r
		}
	}, plain)
	return strings.Join(strings.Fields(plain), " ")
}

// TestRenderErrorOverlay_HasBorderChrome asserts the overlay is wrapped in the
// rounded border chrome (a multi-line bordered box), distinguishing it from a
// bare error string. We check structural properties rather than exact bytes so
// the test stays robust to styling tweaks.
func TestRenderErrorOverlay_HasBorderChrome(t *testing.T) {
	a := &App{err: errors.New("network down")}

	got := a.renderErrorOverlay()

	// The rounded border draws corner runes; their presence proves the box was
	// styled rather than returned as a plain string.
	for _, corner := range []string{"╭", "╮", "╰", "╯"} {
		if !strings.Contains(got, corner) {
			t.Fatalf("expected rounded-border corner %q in overlay, got: %q", corner, got)
		}
	}
	if lines := strings.Count(got, "\n"); lines < 2 {
		t.Fatalf("expected a multi-line bordered overlay, got %d newlines: %q", lines, got)
	}
	// The fixed 56-column width plus border means rendered lines should be wide.
	if w := viewWidth(got); w < 56 {
		t.Fatalf("expected overlay width >= 56 from the fixed-width style, got %d", w)
	}
}

// viewWidth is a small local helper mirroring viewDimensions' width logic so the
// overlay-chrome assertion above does not depend on rendering side effects.
func viewWidth(view string) int {
	w, _ := viewDimensions(view)
	return w
}

// TestFinalizeView_ReturnsViewUnchanged verifies finalizeView is an identity on
// the view it is handed: it must thread the exact content through so callers can
// wrap it in a single tail call without losing the rendered frame.
func TestFinalizeView_ReturnsViewUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
	}{
		{name: "empty content", content: ""},
		{name: "plain content", content: "hello world"},
		{name: "multiline content", content: "line1\nline2\nline3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &App{}
			in := tea.NewView(tc.content)

			out := a.finalizeView(in)

			if out.Content != tc.content {
				t.Fatalf("expected finalizeView to pass content through unchanged, got: %q", out.Content)
			}
		})
	}
}

// TestFinalizeView_ClearsPendingInputLatency confirms the flag-reset side effect:
// when pendingInputLatency is set, finalizeView consumes it (sets it false) so a
// later frame cannot double-record the same input latency sample.
func TestFinalizeView_ClearsPendingInputLatency(t *testing.T) {
	a := &App{pendingInputLatency: true, lastInputAt: time.Now()}

	a.finalizeView(tea.NewView("frame"))

	if a.pendingInputLatency {
		t.Fatal("expected finalizeView to clear pendingInputLatency")
	}
}

// TestFinalizeView_LeavesFlagFalseWhenNotPending verifies the no-op path: with
// pendingInputLatency already false, finalizeView must not flip it on and must
// still return the view.
func TestFinalizeView_LeavesFlagFalseWhenNotPending(t *testing.T) {
	a := &App{pendingInputLatency: false}

	out := a.finalizeView(tea.NewView("frame"))

	if a.pendingInputLatency {
		t.Fatal("expected pendingInputLatency to remain false on the no-op path")
	}
	if out.Content != "frame" {
		t.Fatalf("expected the view to be returned on the no-op path, got: %q", out.Content)
	}
}

// TestFinalizeView_RecordsLatencySampleWhenPending drives the perf side effect.
// With collection forced on and the pending flag set, finalizeView must record
// exactly one "input_latency" duration sample. The no-pending case must record
// nothing, proving the perf.Record call is gated on the flag.
func TestFinalizeView_RecordsLatencySampleWhenPending(t *testing.T) {
	restore := perf.EnableForTest()
	defer restore()
	// Drain any pre-existing samples so the snapshot below is attributable.
	perf.Snapshot()

	a := &App{pendingInputLatency: true, lastInputAt: time.Now().Add(-5 * time.Millisecond)}
	a.finalizeView(tea.NewView("frame"))

	stats, _ := perf.Snapshot()
	sample := findStat(stats, "input_latency")
	if sample == nil {
		t.Fatalf("expected an input_latency perf sample to be recorded, got stats: %+v", stats)
	}
	if sample.Count != 1 {
		t.Fatalf("expected exactly one input_latency sample, got %d", sample.Count)
	}
	if sample.Max <= 0 {
		t.Fatalf("expected a positive recorded latency duration, got %v", sample.Max)
	}
}

// TestFinalizeView_DoesNotRecordWhenNotPending is the negative counterpart: when
// the pending flag is clear, finalizeView must not record any input_latency
// sample even while perf collection is enabled.
func TestFinalizeView_DoesNotRecordWhenNotPending(t *testing.T) {
	restore := perf.EnableForTest()
	defer restore()
	perf.Snapshot()

	a := &App{pendingInputLatency: false}
	a.finalizeView(tea.NewView("frame"))

	stats, _ := perf.Snapshot()
	if sample := findStat(stats, "input_latency"); sample != nil {
		t.Fatalf("expected no input_latency sample on the no-pending path, got: %+v", *sample)
	}
}

// findStat returns the named perf stat snapshot, or nil if absent.
func findStat(stats []perf.StatSnapshot, name string) *perf.StatSnapshot {
	for i := range stats {
		if stats[i].Name == name {
			return &stats[i]
		}
	}
	return nil
}
