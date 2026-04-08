package tmux

import (
	"testing"
)

func TestParsePaneModeState_ParsesCompleteState(t *testing.T) {
	state, match := parsePaneModeState([]string{
		"%1",
		"1",
		"12",
		"7",
		"0",
		"1",
		"2",
		"19",
	}, "%1")
	if !match {
		t.Fatal("expected pane mode state to match target pane")
	}
	if !state.HasState {
		t.Fatal("expected pane mode state to be marked available")
	}
	if !state.AltScreen {
		t.Fatal("expected alternate screen to parse as enabled")
	}
	if !state.OriginMode {
		t.Fatal("expected origin mode to parse as enabled")
	}
	if !state.CursorHidden {
		t.Fatal("expected cursor visibility flag to parse as hidden")
	}
	if state.ScrollTop != 2 || state.ScrollBottom != 20 {
		t.Fatalf("expected scroll region 2..20, got %d..%d", state.ScrollTop, state.ScrollBottom)
	}
	if !state.HasAltSavedCursor {
		t.Fatal("expected alternate-screen saved cursor to parse")
	}
	if state.AltSavedCursorX != 12 || state.AltSavedCursorY != 7 {
		t.Fatalf("expected saved cursor (12,7), got (%d,%d)", state.AltSavedCursorX, state.AltSavedCursorY)
	}
}

func TestParsePaneModeState_MissingOriginAndScrollRegionPreservesAvailableState(t *testing.T) {
	state, match := parsePaneModeState([]string{
		"%1",
		"1",
		"12",
		"7",
		"1",
		"",
		"",
		"",
	}, "%1")
	if !match {
		t.Fatal("expected pane mode state row to match target pane")
	}
	if !state.HasState {
		t.Fatalf("expected missing optional mode fields to keep partial pane mode state, got %+v", state)
	}
	if !state.AltScreen {
		t.Fatal("expected alternate-screen state to survive missing optional fields")
	}
	if state.OriginMode {
		t.Fatal("expected missing origin flag to leave origin mode at the default value")
	}
	if state.ScrollTop != 0 || state.ScrollBottom != 0 {
		t.Fatalf("expected missing scroll-region fields to remain unset, got %d..%d", state.ScrollTop, state.ScrollBottom)
	}
	if !state.HasAltSavedCursor || state.AltSavedCursorX != 12 || state.AltSavedCursorY != 7 {
		t.Fatalf("expected alternate-screen saved cursor to survive partial mode state, got %+v", state)
	}
	if state.CursorHidden {
		t.Fatal("expected visible cursor flag to remain visible")
	}
}

func TestParsePaneModeState_MissingAltScreenReturnsNoModeState(t *testing.T) {
	state, match := parsePaneModeState([]string{
		"%1",
		"",
		"12",
		"7",
		"1",
		"0",
		"0",
		"23",
	}, "%1")
	if !match {
		t.Fatal("expected pane mode state row to match target pane")
	}
	if state.HasState {
		t.Fatalf("expected missing alternate-screen flag to suppress pane mode state, got %+v", state)
	}
}
