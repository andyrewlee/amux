package layout

import "testing"

func TestLayoutModes(t *testing.T) {
	m := NewManager()

	m.Resize(200, 40)
	if m.Mode() != LayoutThreePane {
		t.Fatalf("expected three-pane mode, got %v", m.Mode())
	}
	if !m.ShowSidebar() || !m.ShowCenter() {
		t.Fatalf("expected sidebar and center to be visible")
	}

	m.Resize(100, 40)
	if m.Mode() != LayoutTwoPane {
		t.Fatalf("expected two-pane mode, got %v", m.Mode())
	}
	if m.ShowSidebar() || !m.ShowCenter() {
		t.Fatalf("expected sidebar hidden and center visible")
	}

	m.Resize(50, 40)
	if m.Mode() != LayoutOnePane {
		t.Fatalf("expected one-pane mode, got %v", m.Mode())
	}
	if m.ShowCenter() {
		t.Fatalf("expected center hidden in one-pane mode")
	}
}

func TestLayoutWidthConstraints(t *testing.T) {
	m := NewManager()
	m.Resize(200, 40)

	if m.CenterWidth() < m.minChatWidth {
		t.Fatalf("center width should be >= minChatWidth")
	}
	if m.DashboardWidth() <= 0 {
		t.Fatalf("dashboard width should be > 0")
	}
}
