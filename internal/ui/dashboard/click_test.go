package dashboard

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// setupClickTestModel creates a model with known dimensions and content for click testing.
// The model has:
// - Home row at index 0
// - Spacer row at index 1
// - Project row at index 2
// - Workspace row at index 3
// - Create button at index 4
func setupClickTestModel() *Model {
	m := New()
	m.SetSize(30, 20) // Width 30, Height 20
	m.showKeymapHints = false

	project := data.Project{
		Name: "testproj",
		Path: "/testproj",
		Workspaces: []data.Workspace{
			{Name: "testproj", Branch: "main", Repo: "/testproj", Root: "/testproj"},
			{Name: "feature", Branch: "feature", Repo: "/testproj", Root: "/testproj/.amux/workspaces/feature"},
		},
	}
	m.SetProjects([]data.Project{project})

	// Force a View() call to initialize toolbarY
	_ = m.View()

	return m
}

func TestRowIndexAt(t *testing.T) {
	m := setupClickTestModel()

	// Border is 1 char, padding is 1 char
	// So content X starts at screenX = 2 (border + padding)
	// Content Y starts at screenY = 1 (border)

	// Row layout (0-indexed content Y):
	// 0: [amux] (Home)
	// 1: (Spacer)
	// 2: testproj (Project)
	// 3: feature (Workspace)
	// 4: + New (Create)

	tests := []struct {
		name        string
		screenX     int
		screenY     int
		wantIndex   int
		wantOK      bool
		wantRowType RowType
	}{
		{
			name:        "click on Home row",
			screenX:     5,
			screenY:     1, // content Y = 0
			wantIndex:   0,
			wantOK:      true,
			wantRowType: RowHome,
		},
		{
			name:        "click on Project row",
			screenX:     5,
			screenY:     3, // content Y = 2
			wantIndex:   2,
			wantOK:      true,
			wantRowType: RowProject,
		},
		{
			name:        "click on Workspace row",
			screenX:     5,
			screenY:     4, // content Y = 3
			wantIndex:   3,
			wantOK:      true,
			wantRowType: RowWorkspace,
		},
		{
			name:        "click on Create button",
			screenX:     5,
			screenY:     5, // content Y = 4
			wantIndex:   4,
			wantOK:      true,
			wantRowType: RowCreate,
		},
		{
			name:      "click outside left border",
			screenX:   0,
			screenY:   2,
			wantIndex: -1,
			wantOK:    false,
		},
		{
			name:      "click outside top border",
			screenX:   5,
			screenY:   0,
			wantIndex: -1,
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx, ok := m.rowIndexAt(tt.screenX, tt.screenY)
			if ok != tt.wantOK {
				t.Errorf("rowIndexAt(%d, %d) ok = %v, want %v", tt.screenX, tt.screenY, ok, tt.wantOK)
			}
			if idx != tt.wantIndex {
				t.Errorf("rowIndexAt(%d, %d) index = %d, want %d", tt.screenX, tt.screenY, idx, tt.wantIndex)
			}
			if tt.wantOK && tt.wantIndex >= 0 && tt.wantIndex < len(m.rows) {
				if m.rows[idx].Type != tt.wantRowType {
					t.Errorf("rowIndexAt(%d, %d) row type = %v, want %v", tt.screenX, tt.screenY, m.rows[idx].Type, tt.wantRowType)
				}
			}
		})
	}
}

func TestMouseClickOnRows(t *testing.T) {
	m := setupClickTestModel()

	tests := []struct {
		name         string
		screenX      int
		screenY      int
		wantMsgType  string
		wantSelected int
	}{
		{
			name:         "click Home row triggers ShowWelcome",
			screenX:      5,
			screenY:      1,
			wantMsgType:  "ShowWelcome",
			wantSelected: 0,
		},
		{
			name:         "click Project row triggers WorkspaceActivated",
			screenX:      5,
			screenY:      3,
			wantMsgType:  "WorkspaceActivated",
			wantSelected: 2,
		},
		{
			name:         "click Workspace row triggers WorkspaceActivated",
			screenX:      5,
			screenY:      4,
			wantMsgType:  "WorkspaceActivated",
			wantSelected: 3,
		},
		{
			name:         "click Create button triggers ShowCreateWorkspaceDialog",
			screenX:      5,
			screenY:      5,
			wantMsgType:  "ShowCreateWorkspaceDialog",
			wantSelected: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset model state
			m.cursor = 0
			m.toolbarFocused = false

			// Simulate mouse click
			clickMsg := tea.MouseClickMsg{
				Button: tea.MouseLeft,
				X:      tt.screenX,
				Y:      tt.screenY,
			}

			_, cmd := m.Update(clickMsg)

			if m.cursor != tt.wantSelected {
				t.Errorf("after click, cursor = %d, want %d", m.cursor, tt.wantSelected)
			}

			if cmd == nil {
				t.Fatal("expected command from click, got nil")
			}

			msg := cmd()
			gotType := ""
			switch msg.(type) {
			case messages.ShowWelcome:
				gotType = "ShowWelcome"
			case messages.WorkspaceActivated:
				gotType = "WorkspaceActivated"
			case messages.ShowCreateWorkspaceDialog:
				gotType = "ShowCreateWorkspaceDialog"
			default:
				gotType = "unknown"
			}

			if gotType != tt.wantMsgType {
				t.Errorf("click message type = %s, want %s", gotType, tt.wantMsgType)
			}
		})
	}
}

func TestRowClickWithScrollOffset(t *testing.T) {
	m := New()
	m.SetSize(30, 10) // Small height to force scrolling
	m.showKeymapHints = false

	// Create many projects to exceed visible height
	projects := []data.Project{}
	for i := 0; i < 10; i++ {
		projects = append(projects, data.Project{
			Name: "proj" + string(rune('A'+i)),
			Path: "/proj" + string(rune('A'+i)),
			Workspaces: []data.Workspace{
				{Name: "main", Branch: "main", Repo: "/proj", Root: "/proj"},
			},
		})
	}
	m.SetProjects(projects)
	_ = m.View()

	// Scroll down
	m.scrollOffset = 3

	// Click on first visible row (which is row index 3 due to scroll)
	// Screen Y = 1 (border) maps to content Y = 0, which with scrollOffset=3 is row 3
	idx, ok := m.rowIndexAt(5, 1)
	if !ok {
		t.Fatal("expected valid row index")
	}
	if idx != 3 {
		t.Errorf("with scrollOffset=3, click at content Y=0 should map to row 3, got %d", idx)
	}
}

func TestClickOutsideContentArea(t *testing.T) {
	m := setupClickTestModel()
	_ = m.View()

	tests := []struct {
		name    string
		screenX int
		screenY int
	}{
		{"click on left border", 0, 5},
		{"click on top border", 5, 0},
		{"click beyond right edge", 100, 5},
		{"click beyond bottom edge", 5, 100},
		{"negative X", -1, 5},
		{"negative Y", 5, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx, ok := m.rowIndexAt(tt.screenX, tt.screenY)
			if ok {
				t.Errorf("rowIndexAt(%d, %d) should return ok=false for out-of-bounds click, got index=%d",
					tt.screenX, tt.screenY, idx)
			}
		})
	}
}

// TestClickLowerRowsWithKeymapHintsHidden is a regression test for a bug where
// clicks on lower items in the sidebar failed when showKeymapHints was false.
// The bug was in rowIndexAt() which didn't account for showKeymapHints when
// calculating the clickable area, causing it to subtract help height even when
// help wasn't being rendered.
func TestClickLowerRowsWithKeymapHintsHidden(t *testing.T) {
	m := New()
	m.SetSize(30, 12) // Height chosen so visible rows are affected by help height bug
	m.showKeymapHints = false

	// Create projects to have rows that fill the visible area
	projects := []data.Project{}
	for i := 0; i < 3; i++ {
		projects = append(projects, data.Project{
			Name: "proj" + string(rune('A'+i)),
			Path: "/proj" + string(rune('A'+i)),
			Workspaces: []data.Workspace{
				{Name: "main", Branch: "main", Repo: "/proj", Root: "/proj" + string(rune('A'+i))},
			},
		})
	}
	m.SetProjects(projects)
	_ = m.View()

	// With height=12, innerHeight = 10 (minus 2 for borders)
	// Toolbar takes 1 line, leaving 9 lines for content
	// Help lines (when shown) would be ~3 lines
	// Without the fix, rowIndexAt() would subtract ~3 lines from clickable area
	//
	// Row layout:
	// 0: Home
	// 1: Spacer
	// 2: projA (project)
	// 3: + New (create)
	// 4: Spacer
	// 5: projB (project)
	// 6: + New (create)
	// 7: Spacer
	// 8: projC (project)
	// 9: + New (create)

	// Calculate visible height to find rows near the bottom
	visibleHeight := m.visibleHeight()

	// Try to click on rows that would be in the bottom portion of visible area
	// These would fail without the fix because help height was incorrectly subtracted
	for rowIdx := 0; rowIdx < len(m.rows) && rowIdx < visibleHeight; rowIdx++ {
		screenY := rowIdx + 1 // +1 for top border

		idx, ok := m.rowIndexAt(5, screenY)
		if !ok {
			t.Errorf("row %d at screenY=%d should be clickable, got ok=false", rowIdx, screenY)
			continue
		}
		if idx != rowIdx {
			t.Errorf("click at screenY=%d should map to row %d, got %d", screenY, rowIdx, idx)
		}
	}

	// Specifically test the last visible row - this is where the bug manifests
	lastVisibleRow := visibleHeight - 1
	if lastVisibleRow >= len(m.rows) {
		lastVisibleRow = len(m.rows) - 1
	}
	if lastVisibleRow >= 0 {
		screenY := lastVisibleRow + 1
		idx, ok := m.rowIndexAt(5, screenY)
		if !ok {
			t.Errorf("last visible row %d at screenY=%d should be clickable, got ok=false", lastVisibleRow, screenY)
		}
		if ok && idx != lastVisibleRow {
			t.Errorf("click at screenY=%d should map to row %d, got %d", screenY, lastVisibleRow, idx)
		}
	}
}
