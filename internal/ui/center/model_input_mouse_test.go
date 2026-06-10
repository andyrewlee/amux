package center

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/vterm"
)

func setupMouseReportingTerminal(t *testing.T) *vterm.VTerm {
	t.Helper()
	term := vterm.New(80, 24)
	term.Write([]byte("\x1b[?1000h\x1b[?1006h"))
	if !term.MouseReportingEnabled() || !term.MouseSGRMode() {
		t.Fatal("expected test terminal to enable mouse reporting and SGR mode")
	}
	return term
}

func TestMouseWheelInputSequenceUsesSGRWhenRequested(t *testing.T) {
	term := setupMouseReportingTerminal(t)

	got := mouseWheelInputSequence(term, tea.MouseWheelUp, 2, 3)
	if got != "\x1b[<64;3;4M" {
		t.Fatalf("unexpected SGR wheel sequence: %q", got)
	}
}

func TestMouseWheelInputSequenceUsesX10Fallback(t *testing.T) {
	term := setupMouseReportingTerminal(t)
	term.Write([]byte("\x1b[?1006l"))

	got := mouseWheelInputSequence(term, tea.MouseWheelDown, 2, 3)
	if got != "\x1b[Ma#$" {
		t.Fatalf("unexpected X10 wheel sequence: %q", got)
	}
}

func TestMouseWheelForwardsToMouseReportingTerminalInsteadOfLocalScroll(t *testing.T) {
	m, tab := setupSelectionModel(t)
	m.setTabActorReady()
	m.tabEvents = make(chan tabEvent, 1)

	tab.mu.Lock()
	for i := 0; i < 40; i++ {
		tab.Terminal.Write([]byte("line\n"))
	}
	tab.Terminal.Write([]byte("\x1b[?1000h\x1b[?1006h"))
	tab.mu.Unlock()

	tm := m.terminalMetrics()
	_, _ = m.Update(tea.MouseWheelMsg{
		Button: tea.MouseWheelUp,
		X:      tm.ContentStartX + 2,
		Y:      tm.ContentStartY + 3,
	})

	select {
	case ev := <-m.tabEvents:
		if ev.kind != tabEventSendMouse {
			t.Fatalf("expected mouse input event, got %v", ev.kind)
		}
		if string(ev.input) != "\x1b[<64;3;4M" {
			t.Fatalf("unexpected forwarded wheel input: %q", ev.input)
		}
	default:
		t.Fatal("expected wheel input to be forwarded to tab actor")
	}

	tab.mu.Lock()
	offset, _ := tab.Terminal.GetScrollInfo()
	tab.mu.Unlock()
	if offset != 0 {
		t.Fatalf("expected forwarded wheel to leave local scroll offset at bottom, got %d", offset)
	}
}

func TestCanConsumeWheelWhenTerminalRequestedMouseReporting(t *testing.T) {
	m, tab := setupSelectionModel(t)

	tab.mu.Lock()
	tab.Terminal.Write([]byte("\x1b[?1000h\x1b[?1006h"))
	offset, maxOffset := tab.Terminal.GetScrollInfo()
	tab.mu.Unlock()
	if offset != 0 || maxOffset != 0 {
		t.Fatalf("expected no local scrollback in test setup, got %d/%d", offset, maxOffset)
	}
	if !m.CanConsumeWheel() {
		t.Fatal("expected mouse-reporting terminal to consume wheel without local scrollback")
	}
}

func TestMouseWheelScrollsCapturedNormalScreenChatHistory(t *testing.T) {
	m, tab := setupSelectionModel(t)
	tab.Assistant = "claude"
	m.showKeymapHints = false
	m.SetSize(80, 28)
	m.SetOffset(0)
	tab.mu.Lock()
	tab.Terminal.CaptureNormalScreenOnClear = true
	tab.Terminal.TreatLFAsCRLF = true
	tab.Terminal.Write([]byte("\x1b[?2026hold-frame-00\nold-frame-01\n\x1b[2J\x1b[3Jnew-frame-00\nnew-frame-01\n\x1b[?2026l"))
	offsetBefore, maxBefore := tab.Terminal.GetScrollInfo()
	tab.mu.Unlock()
	if offsetBefore != 0 || maxBefore == 0 {
		t.Fatalf("expected captured history at live bottom, got %d/%d", offsetBefore, maxBefore)
	}

	tm := m.terminalMetrics()
	_, _ = m.Update(tea.MouseWheelMsg{
		Button: tea.MouseWheelUp,
		X:      tm.ContentStartX + 2,
		Y:      tm.ContentStartY + 1,
	})

	tab.mu.Lock()
	offsetAfter, _ := tab.Terminal.GetScrollInfo()
	tab.mu.Unlock()
	if offsetAfter == 0 {
		t.Fatal("expected mouse wheel to scroll captured normal-screen chat history")
	}

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "old-frame-00") {
		t.Fatalf("expected scrolled view to render captured old frame, got %q", view)
	}
}

func TestMouseWheelScrollsTmuxWrappedChatRedrawHistory(t *testing.T) {
	m, tab := setupSelectionModel(t)
	tab.Assistant = "claude"
	m.showKeymapHints = false
	m.SetSize(80, 28)
	m.SetOffset(0)
	tab.mu.Lock()
	tab.Terminal.AltScreen = true
	tab.Terminal.AllowAltScreenScrollback = true
	tab.Terminal.CaptureNormalScreenOnClear = true
	tab.Terminal.TreatLFAsCRLF = true
	tab.Terminal.Write([]byte("old-frame-00\nold-frame-01\n\x1b[H\x1b[Jnew-frame-00\nnew-frame-01\n"))
	offsetBefore, maxBefore := tab.Terminal.GetScrollInfo()
	tab.mu.Unlock()
	if offsetBefore != 0 || maxBefore == 0 {
		t.Fatalf("expected captured tmux-wrapped history at live bottom, got %d/%d", offsetBefore, maxBefore)
	}

	tm := m.terminalMetrics()
	_, _ = m.Update(tea.MouseWheelMsg{
		Button: tea.MouseWheelUp,
		X:      tm.ContentStartX + 2,
		Y:      tm.ContentStartY + 1,
	})

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "old-frame-00") {
		t.Fatalf("expected scrolled tmux-wrapped view to render captured old frame, got %q", view)
	}
}
