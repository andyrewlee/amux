package center

import "testing"

func TestMonitorModelSelectedIndexClamps(t *testing.T) {
	var m MonitorModel

	m.SetSelectedIndex(-3, 5)
	if got := m.SelectedIndex(5); got != 0 {
		t.Fatalf("expected clamp to 0, got %d", got)
	}

	m.SetSelectedIndex(10, 5)
	if got := m.SelectedIndex(5); got != 4 {
		t.Fatalf("expected clamp to 4, got %d", got)
	}

	m.SetSelectedIndex(2, 5)
	if got := m.SelectedIndex(5); got != 2 {
		t.Fatalf("expected 2, got %d", got)
	}
}

func TestMonitorModelMoveSelection(t *testing.T) {
	var m MonitorModel

	m.SetSelectedIndex(0, 3)
	m.MoveSelection(1, 0, 2, 2, 3)
	if got := m.SelectedIndex(3); got != 1 {
		t.Fatalf("expected index 1, got %d", got)
	}

	m.MoveSelection(0, 1, 2, 2, 3)
	if got := m.SelectedIndex(3); got != 2 {
		t.Fatalf("expected index 2, got %d", got)
	}

	// Moving right on the last row should clamp to last item.
	m.MoveSelection(1, 0, 2, 2, 3)
	if got := m.SelectedIndex(3); got != 2 {
		t.Fatalf("expected clamp to 2, got %d", got)
	}
}
