package app

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
)

func newFrameCacheHarness(t *testing.T, tabs int) *Harness {
	t.Helper()
	h, err := NewHarness(HarnessOptions{
		Mode:   HarnessCenter,
		Tabs:   tabs,
		Width:  160,
		Height: 48,
	})
	if err != nil {
		t.Fatalf("frame-cache harness init: %v", err)
	}
	h.app.ready = true
	h.app.syncPaneFocusFlags()
	return h
}

func TestFullFrameCache_ReusesFrameForRenderNeutralPTYMessages(t *testing.T) {
	h := newFrameCacheHarness(t, 1)

	first := h.app.view()
	builds := h.app.renderCache.frame.builds
	if builds == 0 {
		t.Fatal("first view did not populate the full-frame cache")
	}

	_, _ = h.app.Update(center.PTYOutput{
		WorkspaceID: "missing-workspace",
		TabID:       center.TabID("missing-tab"),
		Data:        []byte("buffered output"),
	})
	second := h.app.view()

	if got := h.app.renderCache.frame.builds; got != builds {
		t.Fatalf("render-neutral PTY output rebuilt full frame: builds %d -> %d", builds, got)
	}
	if h.app.renderCache.frame.hits == 0 {
		t.Fatal("render-neutral PTY output did not hit the full-frame cache")
	}
	if second.Content != first.Content {
		t.Fatal("cached frame content changed after render-neutral PTY output")
	}
}

func TestFullFrameCache_ActiveTerminalVersionForcesRebuild(t *testing.T) {
	h := newFrameCacheHarness(t, 1)

	first := h.app.view()
	builds := h.app.renderCache.frame.builds
	h.tabs[0].WriteToTerminal([]byte("active mutation"))
	second := h.app.view()

	if got := h.app.renderCache.frame.builds; got != builds+1 {
		t.Fatalf("active terminal mutation did not rebuild full frame: builds %d -> %d", builds, got)
	}
	if second.Content == first.Content {
		t.Fatal("active terminal mutation reused stale frame content")
	}
}

func TestFullFrameCache_ActiveTerminalTitleForcesRebuild(t *testing.T) {
	h := newFrameCacheHarness(t, 1)

	_ = h.app.view()
	builds := h.app.renderCache.frame.builds
	// OSC title only: no cell changes, so the content version stays put while the
	// window title the frame carries does not.
	h.tabs[0].WriteToTerminal([]byte("\x1b]0;agent working\x07"))
	_ = h.app.view()

	if got := h.app.renderCache.frame.builds; got != builds+1 {
		t.Fatalf("terminal title change did not rebuild full frame: builds %d -> %d", builds, got)
	}
}

func TestFullFrameCache_InactiveTerminalVersionReusesFrame(t *testing.T) {
	h := newFrameCacheHarness(t, 2)
	_ = h.app.center.SelectTab(0)

	first := h.app.view()
	builds := h.app.renderCache.frame.builds
	h.tabs[1].WriteToTerminal([]byte("inactive mutation"))
	second := h.app.view()

	if got := h.app.renderCache.frame.builds; got != builds {
		t.Fatalf("inactive terminal mutation rebuilt full frame: builds %d -> %d", builds, got)
	}
	if second.Content != first.Content {
		t.Fatal("inactive terminal mutation changed the visible frame")
	}
}

// TestFullFrameCache_BackgroundTabActivityForcesRebuild is the regression the
// activity key exists for: the tab bar highlights a background chat tab that is
// working, and nothing about that reaches the active terminal's content.
func TestFullFrameCache_BackgroundTabActivityForcesRebuild(t *testing.T) {
	h := newFrameCacheHarness(t, 2)
	_ = h.app.center.SelectTab(0)

	first := h.app.view()
	builds := h.app.renderCache.frame.builds
	h.tabs[1].NoteVisibleOutput(time.Now())
	second := h.app.view()

	if got := h.app.renderCache.frame.builds; got != builds+1 {
		t.Fatalf("background tab activity did not rebuild full frame: builds %d -> %d", builds, got)
	}
	if second.Content == first.Content {
		t.Fatal("background tab activity left the tab bar unchanged")
	}
}

func TestFullFrameCache_OrdinaryMessageInvalidatesFrame(t *testing.T) {
	h := newFrameCacheHarness(t, 1)

	_ = h.app.view()
	builds := h.app.renderCache.frame.builds
	_, _ = h.app.Update(tea.WindowSizeMsg{Width: 161, Height: 48})
	_ = h.app.view()

	if got := h.app.renderCache.frame.builds; got != builds+1 {
		t.Fatalf("ordinary visible message did not rebuild full frame: builds %d -> %d", builds, got)
	}
}

func TestFrameInvalidatedBy_HighFrequencyPTYMessagesAreNeutral(t *testing.T) {
	neutral := []tea.Msg{
		center.PTYOutput{},
		center.PTYFlush{},
		messages.SidebarPTYOutput{},
		messages.SidebarPTYFlush{},
	}
	for _, msg := range neutral {
		if frameInvalidatedBy(msg) {
			t.Fatalf("expected %T to be render-neutral", msg)
		}
	}
	if !frameInvalidatedBy(center.PTYStopped{}) {
		t.Fatal("PTYStopped must invalidate the frame")
	}
}
