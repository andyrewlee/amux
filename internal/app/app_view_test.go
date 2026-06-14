package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
)

// TestFallbackView_DefaultMessageWithoutError verifies the generic copy used
// when the recovered render error has not populated a.err yet.
func TestFallbackView_DefaultMessageWithoutError(t *testing.T) {
	a := &App{}

	view := a.fallbackView()

	if !strings.Contains(view.Content, "A rendering error occurred.") {
		t.Fatalf("expected default render-error message, got: %q", view.Content)
	}
	if strings.Contains(view.Content, "Error:") {
		t.Fatalf("did not expect an Error: prefix when a.err is nil, got: %q", view.Content)
	}
	if !strings.HasSuffix(view.Content, "Press any key to dismiss.") {
		t.Fatalf("expected dismiss hint suffix, got: %q", view.Content)
	}
}

// TestFallbackView_RendersErrorMessage verifies a populated a.err is surfaced
// verbatim with the "Error: " prefix instead of the generic copy.
func TestFallbackView_RendersErrorMessage(t *testing.T) {
	a := &App{err: errors.New("boom: layer overflow")}

	view := a.fallbackView()

	if !strings.Contains(view.Content, "Error: boom: layer overflow") {
		t.Fatalf("expected the concrete error text, got: %q", view.Content)
	}
	if strings.Contains(view.Content, "A rendering error occurred.") {
		t.Fatalf("did not expect the generic message when a.err is set, got: %q", view.Content)
	}
	if !strings.HasSuffix(view.Content, "Press any key to dismiss.") {
		t.Fatalf("expected dismiss hint suffix, got: %q", view.Content)
	}
}

// TestFallbackView_TerminalChromeFields asserts the fallback view always opts
// into the alt-screen and sets explicit fore/background colors regardless of
// whether an error is present (it must remain a self-contained full-screen
// frame even on the error path).
func TestFallbackView_TerminalChromeFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "nil error", err: nil},
		{name: "with error", err: errors.New("x")},
		{name: "empty error string", err: errors.New("")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &App{err: tc.err}

			view := a.fallbackView()

			if !view.AltScreen {
				t.Fatal("expected fallback view to request the alt screen")
			}
			if view.BackgroundColor != common.ColorBackground() {
				t.Fatal("expected fallback background color to match the theme background")
			}
			if view.ForegroundColor != common.ColorForeground() {
				t.Fatal("expected fallback foreground color to match the theme foreground")
			}
			if view.Content == "" {
				t.Fatal("expected fallback view to carry content")
			}
		})
	}
}

// TestView_QuittingBranch covers the goodbye state of view(): it must short
// circuit to a base view carrying the goodbye banner without touching the
// layer-based renderer (which would need a fully wired App).
func TestView_QuittingBranch(t *testing.T) {
	a := &App{quitting: true}

	view := a.view()

	if view.Content != "Goodbye!\n" {
		t.Fatalf("expected goodbye banner, got: %q", view.Content)
	}
	assertBaseViewChrome(t, view)
}

// TestView_LoadingBranch covers the not-ready state of view(): before the app
// finishes initializing it renders a loading placeholder via the base view.
func TestView_LoadingBranch(t *testing.T) {
	a := &App{ready: false}

	view := a.view()

	if view.Content != "Loading..." {
		t.Fatalf("expected loading placeholder, got: %q", view.Content)
	}
	assertBaseViewChrome(t, view)
}

// TestView_QuittingTakesPrecedenceOverReady ensures the quitting check runs
// before the readiness check: a ready+quitting app still says goodbye.
func TestView_QuittingTakesPrecedenceOverReady(t *testing.T) {
	a := &App{ready: true, quitting: true}

	view := a.view()

	if view.Content != "Goodbye!\n" {
		t.Fatalf("expected quitting to win over ready, got: %q", view.Content)
	}
}

// TestView_FinalizeClearsPendingInputLatency confirms the early-return branches
// route through finalizeView, which resets the pending input-latency flag so
// the next frame does not double-record a latency sample.
func TestView_FinalizeClearsPendingInputLatency(t *testing.T) {
	for _, tc := range []struct {
		name string
		app  *App
	}{
		{name: "quitting", app: &App{quitting: true, pendingInputLatency: true, lastInputAt: time.Now()}},
		{name: "loading", app: &App{ready: false, pendingInputLatency: true, lastInputAt: time.Now()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_ = tc.app.view()
			if tc.app.pendingInputLatency {
				t.Fatal("expected view() to clear pendingInputLatency via finalizeView")
			}
		})
	}
}

// TestView_ReadyBranchRendersLayerComposition drives the happy path of view()
// through the headless harness (which wires a real App), proving that a ready,
// non-quitting app renders the layer-based frame wrapped in the DEC 2026
// synchronized-output markers rather than a placeholder banner.
func TestView_ReadyBranchRendersLayerComposition(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Tabs: 1, Width: 160, Height: 48})
	if err != nil {
		t.Fatalf("expected harness creation to succeed: %v", err)
	}
	h.app.ready = true
	h.app.syncPaneFocusFlags()

	view := h.app.view()

	if view.Content == "Goodbye!\n" || view.Content == "Loading..." {
		t.Fatalf("expected layer-based render, got placeholder: %q", view.Content)
	}
	if !strings.HasPrefix(view.Content, syncBegin) || !strings.HasSuffix(view.Content, syncEnd) {
		t.Fatal("expected the ready frame to be wrapped in DEC 2026 sync markers")
	}
}

// TestPublicView_DelegatesToView confirms View() forwards to view() on the
// normal (non-panicking) path: a quitting app produces the same goodbye frame
// through the public entry point as through the internal one.
func TestPublicView_DelegatesToView(t *testing.T) {
	a := &App{quitting: true}

	view := a.View()

	if view.Content != "Goodbye!\n" {
		t.Fatalf("expected View() to delegate to view(), got: %q", view.Content)
	}
	if a.err != nil {
		t.Fatalf("did not expect View() to set an error on the happy path, got: %v", a.err)
	}
}

// TestPublicView_RecoversPanicIntoFallback exercises the deferred recover in
// View(): a ready app with no layout panics inside the layer-based renderer
// (nil *layout.Manager dereference). View() must trap that panic, stash a
// render error on a.err, and return the fallback frame instead of crashing.
func TestPublicView_RecoversPanicIntoFallback(t *testing.T) {
	a := &App{ready: true} // ready, not quitting, but layout/components are nil => panic

	view := a.View()

	if a.err == nil {
		t.Fatal("expected View() to record a render error after recovering the panic")
	}
	if !strings.Contains(a.err.Error(), "render error") {
		t.Fatalf("expected a wrapped render error, got: %v", a.err)
	}
	// The returned frame must be the fallback view, which surfaces the recorded
	// error text and the dismiss hint.
	if !strings.Contains(view.Content, "Error: render error") {
		t.Fatalf("expected fallback content to echo the render error, got: %q", view.Content)
	}
	if !strings.HasSuffix(view.Content, "Press any key to dismiss.") {
		t.Fatalf("expected fallback dismiss hint, got: %q", view.Content)
	}
	if !view.AltScreen {
		t.Fatal("expected the recovered fallback frame to still request the alt screen")
	}
}

// assertBaseViewChrome checks the shared terminal-chrome fields that view()'s
// baseView() helper stamps onto every early-return frame.
func assertBaseViewChrome(t *testing.T, view tea.View) {
	t.Helper()
	if !view.AltScreen {
		t.Fatal("expected base view to request the alt screen")
	}
	if view.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("expected cell-motion mouse mode, got: %v", view.MouseMode)
	}
	if view.BackgroundColor != common.ColorBackground() {
		t.Fatal("expected base background color to match the theme background")
	}
	if view.ForegroundColor != common.ColorForeground() {
		t.Fatal("expected base foreground color to match the theme foreground")
	}
	if !view.KeyboardEnhancements.ReportEventTypes {
		t.Fatal("expected base view to request keyboard event-type reporting")
	}
}
