package dashboard

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestToolbarClick(t *testing.T) {
	m := setupClickTestModel()

	// Force View to calculate toolbarY
	_ = m.View()

	// The toolbar should be at the bottom of the content area
	// With showKeymapHints=false, toolbar is rendered without help lines below
	// Toolbar buttons are: [Commands] [Settings] on a single row

	// Get toolbar Y position (set during View())
	// We need to add border offset (1) to get screen Y
	toolbarScreenY := m.toolbarY + 1 // +1 for top border

	tests := []struct {
		name        string
		screenX     int
		screenY     int
		wantMsgType string
	}{
		{
			name:        "click Commands button",
			screenX:     3, // [Commands] starts near the left
			screenY:     toolbarScreenY,
			wantMsgType: "ShowCommandsPalette",
		},
		{
			name:        "click Settings button",
			screenX:     13, // [Settings] is after [Commands] with gap
			screenY:     toolbarScreenY,
			wantMsgType: "ShowSettingsDialog",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset state
			m.toolbarFocused = false
			m.toolbarIndex = 0

			cmd := m.handleToolbarClick(tt.screenX, tt.screenY)
			if cmd == nil {
				t.Fatalf("expected command from toolbar click at (%d, %d), got nil (toolbarY=%d)",
					tt.screenX, tt.screenY, m.toolbarY)
			}

			msg := cmd()
			gotType := ""
			switch msg.(type) {
			case messages.ShowCommandsPalette:
				gotType = "ShowCommandsPalette"
			case messages.ShowSettingsDialog:
				gotType = "ShowSettingsDialog"
			default:
				gotType = "unknown"
			}

			if gotType != tt.wantMsgType {
				t.Errorf("toolbar click message type = %s, want %s", gotType, tt.wantMsgType)
			}
			if m.toolbarFocused {
				t.Errorf("toolbar click should not leave toolbarFocused=true")
			}
		})
	}
}

func TestToolbarEnterDispatchClearsToolbarFocus(t *testing.T) {
	m := setupClickTestModel()
	m.toolbarFocused = true
	m.toolbarIndex = 0

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command when pressing enter on focused toolbar item")
	}
	if m.toolbarFocused {
		t.Fatal("expected toolbar focus to clear after dispatching toolbar command")
	}
	msg := cmd()
	if _, ok := msg.(messages.ShowCommandsPalette); !ok {
		t.Fatalf("expected ShowCommandsPalette message, got %T", msg)
	}
}

func TestToolbarYCalculation(t *testing.T) {
	// Test that toolbarY is correctly calculated for different content sizes

	t.Run("few rows - toolbar pushed to bottom", func(t *testing.T) {
		m := New()
		m.SetSize(30, 20)
		m.showKeymapHints = false
		m.SetProjects([]data.Project{{
			Name: "test",
			Path: "/test",
			Workspaces: []data.Workspace{
				{Name: "main", Branch: "main", Root: "/test"},
			},
		}})

		_ = m.View()

		// With few rows, toolbar should be near the bottom
		// innerHeight = 20 - 2 = 18
		// toolbarHeight = 1 (single row of buttons)
		// targetHeight = 18 - 1 = 17
		// Toolbar should be at or near targetHeight
		if m.toolbarY < 5 {
			t.Errorf("toolbarY = %d, expected it to be pushed toward bottom (>= 5)", m.toolbarY)
		}
	})

	t.Run("many rows - toolbar follows content", func(t *testing.T) {
		m := New()
		m.SetSize(30, 10) // Small height
		m.showKeymapHints = false

		// Create many projects
		projects := []data.Project{}
		for i := 0; i < 20; i++ {
			projects = append(projects, data.Project{
				Name: "proj" + string(rune('A'+i)),
				Path: "/proj",
				Workspaces: []data.Workspace{
					{Name: "main", Branch: "main", Root: "/proj"},
				},
			})
		}
		m.SetProjects(projects)

		_ = m.View()

		// Toolbar should still be clickable
		if m.toolbarY < 0 {
			t.Errorf("toolbarY = %d, should be non-negative", m.toolbarY)
		}
	})
}

// TestDeleteButtonClick exercises the real delete affordance: the "x" icon
// rendered on the selected row (deleteIconX, recorded during View). The
// toolbar itself only carries Commands/Settings — a click at the recorded
// icon slot must route to handleDelete and emit ShowDeleteWorkspaceDialog.
func TestDeleteButtonClick(t *testing.T) {
	m := setupClickTestModel()
	m.focused = true
	m.cursor = 3 // Workspace row
	_ = m.View()

	if m.deleteIconX <= 0 {
		t.Fatalf("delete icon position not recorded for selected workspace row (deleteIconX=%d)", m.deleteIconX)
	}
	screenY := m.cursor - m.scrollOffset + 1 // +1 for top border
	// contentX = X - borderLeft(1); aim one cell into the [x, x+3) icon slot.
	_, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: m.deleteIconX + 2, Y: screenY})
	if cmd == nil {
		t.Fatalf("click on delete icon at (%d, %d) produced no command", m.deleteIconX+2, screenY)
	}
	msg := cmd()
	if _, ok := msg.(messages.ShowDeleteWorkspaceDialog); !ok {
		t.Fatalf("delete icon click produced %T, want ShowDeleteWorkspaceDialog", msg)
	}

	// One cell left of the icon slot is a plain row click, not a delete —
	// without this the test can't tell a too-wide slot from a real hit.
	m2 := setupClickTestModel()
	m2.focused = true
	m2.cursor = 3
	_ = m2.View()
	_, cmd2 := m2.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: m2.deleteIconX, Y: screenY})
	if cmd2 != nil {
		if _, ok := cmd2().(messages.ShowDeleteWorkspaceDialog); ok {
			t.Fatal("click left of the icon slot produced ShowDeleteWorkspaceDialog; slot window too wide")
		}
	}
}

func TestRemoveButtonClickOnProject(t *testing.T) {
	m := setupClickTestModel()
	m.focused = true
	m.cursor = 2 // Project row
	_ = m.View()

	if m.deleteIconX <= 0 {
		t.Fatalf("delete icon position not recorded for selected project row (deleteIconX=%d)", m.deleteIconX)
	}
	screenY := m.cursor - m.scrollOffset + 1 // +1 for top border
	_, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: m.deleteIconX + 2, Y: screenY})
	if cmd == nil {
		t.Fatalf("click on remove icon at (%d, %d) produced no command", m.deleteIconX+2, screenY)
	}
	msg := cmd()
	if _, ok := msg.(messages.ShowRemoveProjectDialog); !ok {
		t.Fatalf("remove icon click produced %T, want ShowRemoveProjectDialog", msg)
	}
}
