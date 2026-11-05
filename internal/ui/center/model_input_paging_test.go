package center

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Unmodified PgUp/PgDn belong to the hosted application when it owns the
// viewport (alternate screen); amux keeps them for its own history on the
// primary screen. Modified combinations stay amux-local in both cases,
// mirroring the shift+wheel force-local convention.

func TestPagingKeysForwardToAltScreenApplication(t *testing.T) {
	m, tab := setupSelectionModel(t)
	tab.mu.Lock()
	tab.Terminal.SetPaneAlternateScreen(true)
	tab.paneStateProbedAt = time.Now()
	tab.mu.Unlock()

	_, _, handled := m.handleScrollbackKey(tea.KeyPressMsg{Code: tea.KeyPgUp}, tab)
	if !handled {
		t.Fatal("expected PgUp to be claimed (forwarded to the application)")
	}
	tab.mu.Lock()
	offset, _ := tab.Terminal.GetScrollInfo()
	tab.mu.Unlock()
	if offset != 0 {
		t.Fatalf("alt-screen PgUp must not scroll locally, got offset %d", offset)
	}
}

func TestPagingKeysScrollPrimaryScreenHistory(t *testing.T) {
	m, tab := setupSelectionModel(t)
	tab.mu.Lock()
	tab.Terminal.Write([]byte(strings.Repeat("line\n", 40)))
	tab.mu.Unlock()

	_, _, handled := m.handleScrollbackKey(tea.KeyPressMsg{Code: tea.KeyPgUp}, tab)
	if !handled {
		t.Fatal("expected PgUp to be claimed (local history scroll)")
	}
	tab.mu.Lock()
	offset, _ := tab.Terminal.GetScrollInfo()
	tab.mu.Unlock()
	if offset == 0 {
		t.Fatal("expected PgUp to scroll primary-screen history")
	}
}

func TestModifiedPagingKeysStayLocalOnAltScreen(t *testing.T) {
	m, tab := setupSelectionModel(t)
	tab.mu.Lock()
	tab.Terminal.SetPaneAlternateScreen(true)
	tab.paneStateProbedAt = time.Now()
	tab.mu.Unlock()

	msg := tea.KeyPressMsg{Code: tea.KeyPgUp, Mod: tea.ModShift}
	_, _, handled := m.handleScrollbackKey(msg, tab)
	if !handled {
		t.Fatal("expected shift+PgUp to be claimed locally")
	}
	tab.mu.Lock()
	offset, _ := tab.Terminal.GetScrollInfo()
	tab.mu.Unlock()
	if offset != 0 {
		t.Fatalf("shift+PgUp on an alt-screen pane has nothing to scroll, got offset %d", offset)
	}
}
