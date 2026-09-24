package sidebar

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
)

func TestProjectTreeUpdateMouseWheelMovesCursor(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	m.SetSize(40, 20)
	m.cursor = 2

	_, cmd := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if cmd != nil {
		t.Fatalf("expected nil cmd for wheel up, got %v", cmd)
	}
	if m.cursor >= 2 {
		t.Fatalf("expected wheel up to decrease cursor, got %d", m.cursor)
	}

	up := m.cursor
	_, cmd = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if cmd != nil {
		t.Fatalf("expected nil cmd for wheel down, got %v", cmd)
	}
	if m.cursor <= up {
		t.Fatalf("expected wheel down to increase cursor from %d, got %d", up, m.cursor)
	}
}

func TestProjectTreeUpdateMouseClickSelectsRow(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	m.SetSize(40, 20)
	m.SetShowKeymapHints(false)
	m.cursor = 0

	// A left click on screen row 2 maps to flat node index 2 (one.txt), which
	// emits an open command.
	_, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, Y: 2})
	if m.cursor != 2 {
		t.Fatalf("expected click to move cursor to row 2, got %d", m.cursor)
	}
	if cmd == nil {
		t.Fatal("expected open command from clicking a file row")
	}
	msg := cmd()
	if _, ok := msg.(messages.OpenFileInVim); !ok {
		t.Fatalf("expected OpenFileInVim from click, got %T", msg)
	}
}

func TestProjectTreeUpdateMouseClickOutsideRowsIsNoop(t *testing.T) {
	m := newSeededProjectTree(t)
	m.Focus()
	m.SetSize(40, 20)
	m.SetShowKeymapHints(false)
	m.cursor = 1

	// Clicking far below the last node resolves to no row; cursor must stay put.
	_, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, Y: 50})
	if cmd != nil {
		t.Fatalf("expected nil cmd for click outside rows, got %v", cmd)
	}
	if m.cursor != 1 {
		t.Fatalf("expected cursor unchanged on missed click, got %d", m.cursor)
	}
}
