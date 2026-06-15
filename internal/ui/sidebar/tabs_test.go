package sidebar

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// newTestTabbedSidebar returns a sidebar sized so View/ContentView render content.
func newTestTabbedSidebar(t *testing.T) *TabbedSidebar {
	t.Helper()
	s := NewTabbedSidebar()
	if s == nil {
		t.Fatal("NewTabbedSidebar returned nil")
	}
	return s
}

func TestNewTabbedSidebarDefaults(t *testing.T) {
	s := NewTabbedSidebar()

	if s == nil {
		t.Fatal("expected non-nil sidebar")
	}
	if s.activeTab != TabChanges {
		t.Fatalf("expected default active tab TabChanges, got %d", s.activeTab)
	}
	if s.changes == nil {
		t.Fatal("expected non-nil changes model")
	}
	if s.projectTree == nil {
		t.Fatal("expected non-nil project tree model")
	}
	if s.Changes() != s.changes {
		t.Fatal("Changes() should return the inner changes model")
	}
	if s.ProjectTree() != s.projectTree {
		t.Fatal("ProjectTree() should return the inner project tree model")
	}
	if s.Focused() {
		t.Fatal("expected new sidebar to start unfocused")
	}
	if s.ActiveTab() != TabChanges {
		t.Fatalf("ActiveTab() = %d, want TabChanges", s.ActiveTab())
	}
}

func TestSetShowKeymapHintsPropagates(t *testing.T) {
	tests := []struct {
		name string
		show bool
	}{
		{name: "enable hints", show: true},
		{name: "disable hints", show: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestTabbedSidebar(t)
			s.SetShowKeymapHints(tc.show)

			if s.showKeymapHints != tc.show {
				t.Fatalf("sidebar showKeymapHints = %v, want %v", s.showKeymapHints, tc.show)
			}
			if s.changes.showKeymapHints != tc.show {
				t.Fatalf("changes showKeymapHints = %v, want %v", s.changes.showKeymapHints, tc.show)
			}
			if s.projectTree.showKeymapHints != tc.show {
				t.Fatalf("projectTree showKeymapHints = %v, want %v", s.projectTree.showKeymapHints, tc.show)
			}
		})
	}
}

func TestSetStylesPropagates(t *testing.T) {
	s := newTestTabbedSidebar(t)

	// Build a recognizably distinct Styles value so we can prove propagation
	// rather than relying on the default identity.
	styles := common.DefaultStyles()
	styles.Muted = styles.Muted.SetString("MUTED-MARKER")

	s.SetStyles(styles)

	if s.styles.Muted.Value() != "MUTED-MARKER" {
		t.Fatalf("sidebar styles not updated, got %q", s.styles.Muted.Value())
	}
	if s.changes.styles.Muted.Value() != "MUTED-MARKER" {
		t.Fatalf("changes styles not updated, got %q", s.changes.styles.Muted.Value())
	}
	if s.projectTree.styles.Muted.Value() != "MUTED-MARKER" {
		t.Fatalf("projectTree styles not updated, got %q", s.projectTree.styles.Muted.Value())
	}
}

func TestInitReturnsNilWhenInnerModelsIdle(t *testing.T) {
	s := newTestTabbedSidebar(t)

	// Both inner Init() return nil, so SafeBatch collapses to nil.
	if cmd := s.Init(); cmd != nil {
		t.Fatalf("expected nil cmd from Init when inner models are idle, got %T", cmd)
	}
}

func TestFocusBlurFocused(t *testing.T) {
	s := newTestTabbedSidebar(t)

	if s.Focused() {
		t.Fatal("expected unfocused initially")
	}

	s.Focus()
	if !s.Focused() {
		t.Fatal("expected focused after Focus()")
	}
	// On the Changes tab, focus must route to the changes model only.
	if !s.changes.Focused() {
		t.Fatal("expected changes model focused on Changes tab")
	}
	if s.projectTree.Focused() {
		t.Fatal("expected project tree blurred on Changes tab")
	}

	s.Blur()
	if s.Focused() {
		t.Fatal("expected unfocused after Blur()")
	}
	if s.changes.Focused() || s.projectTree.Focused() {
		t.Fatal("expected both inner models blurred after Blur()")
	}
}

func TestUpdateFocusRoutesToActiveTab(t *testing.T) {
	tests := []struct {
		name        string
		focused     bool
		active      SidebarTab
		wantChanges bool
		wantProject bool
	}{
		{name: "focused on changes", focused: true, active: TabChanges, wantChanges: true, wantProject: false},
		{name: "focused on project", focused: true, active: TabProject, wantChanges: false, wantProject: true},
		{name: "unfocused on changes", focused: false, active: TabChanges, wantChanges: false, wantProject: false},
		{name: "unfocused on project", focused: false, active: TabProject, wantChanges: false, wantProject: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestTabbedSidebar(t)
			s.focused = tc.focused
			s.activeTab = tc.active

			s.updateFocus()

			if s.changes.Focused() != tc.wantChanges {
				t.Fatalf("changes focused = %v, want %v", s.changes.Focused(), tc.wantChanges)
			}
			if s.projectTree.Focused() != tc.wantProject {
				t.Fatalf("projectTree focused = %v, want %v", s.projectTree.Focused(), tc.wantProject)
			}
		})
	}
}

func TestActiveTabAndSetActiveTab(t *testing.T) {
	s := newTestTabbedSidebar(t)
	s.Focus()

	s.SetActiveTab(TabProject)
	if s.ActiveTab() != TabProject {
		t.Fatalf("ActiveTab() = %d, want TabProject", s.ActiveTab())
	}
	// Focus must follow the active tab.
	if !s.projectTree.Focused() || s.changes.Focused() {
		t.Fatal("focus did not follow SetActiveTab(TabProject)")
	}

	s.SetActiveTab(TabChanges)
	if s.ActiveTab() != TabChanges {
		t.Fatalf("ActiveTab() = %d, want TabChanges", s.ActiveTab())
	}
	if !s.changes.Focused() || s.projectTree.Focused() {
		t.Fatal("focus did not follow SetActiveTab(TabChanges)")
	}
}

func TestNextAndPrevTabCircular(t *testing.T) {
	s := newTestTabbedSidebar(t)

	if s.ActiveTab() != TabChanges {
		t.Fatalf("precondition: want TabChanges, got %d", s.ActiveTab())
	}

	s.NextTab()
	if s.ActiveTab() != TabProject {
		t.Fatalf("after NextTab want TabProject, got %d", s.ActiveTab())
	}
	s.NextTab()
	if s.ActiveTab() != TabChanges {
		t.Fatalf("after second NextTab want wrap to TabChanges, got %d", s.ActiveTab())
	}

	s.PrevTab()
	if s.ActiveTab() != TabProject {
		t.Fatalf("after PrevTab want TabProject, got %d", s.ActiveTab())
	}
	s.PrevTab()
	if s.ActiveTab() != TabChanges {
		t.Fatalf("after second PrevTab want wrap to TabChanges, got %d", s.ActiveTab())
	}
}

func TestSetSizePropagatesAndClamps(t *testing.T) {
	tests := []struct {
		name            string
		width           int
		height          int
		wantInnerWidth  int
		wantInnerHeight int
	}{
		{name: "normal", width: 40, height: 20, wantInnerWidth: 40, wantInnerHeight: 19},
		{name: "height one leaves zero content", width: 30, height: 1, wantInnerWidth: 30, wantInnerHeight: 0},
		{name: "zero height clamps content to zero", width: 30, height: 0, wantInnerWidth: 30, wantInnerHeight: 0},
		{name: "negative height clamps content to zero", width: 25, height: -5, wantInnerWidth: 25, wantInnerHeight: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestTabbedSidebar(t)
			s.SetSize(tc.width, tc.height)

			if s.width != tc.width || s.height != tc.height {
				t.Fatalf("sidebar size = %dx%d, want %dx%d", s.width, s.height, tc.width, tc.height)
			}
			if s.changes.width != tc.wantInnerWidth || s.changes.height != tc.wantInnerHeight {
				t.Fatalf("changes inner size = %dx%d, want %dx%d",
					s.changes.width, s.changes.height, tc.wantInnerWidth, tc.wantInnerHeight)
			}
			if s.projectTree.width != tc.wantInnerWidth || s.projectTree.height != tc.wantInnerHeight {
				t.Fatalf("projectTree inner size = %dx%d, want %dx%d",
					s.projectTree.width, s.projectTree.height, tc.wantInnerWidth, tc.wantInnerHeight)
			}
		})
	}
}

func TestSetWorkspacePropagatesToInnerModels(t *testing.T) {
	s := newTestTabbedSidebar(t)
	ws := data.NewWorkspace("ws", "feature", "main", "/repo", "/repo/ws")

	s.SetWorkspace(ws)

	if s.workspace != ws {
		t.Fatal("sidebar workspace not set")
	}
	if s.changes.workspace != ws {
		t.Fatal("changes workspace not set")
	}
	if s.projectTree.workspace != ws {
		t.Fatal("projectTree workspace not set")
	}
}

func TestSetWorkspaceNilIsSafe(t *testing.T) {
	s := newTestTabbedSidebar(t)
	s.SetWorkspace(nil)

	if s.workspace != nil {
		t.Fatal("expected nil workspace")
	}
	// View must still render (no crash) with a nil workspace.
	s.SetSize(40, 20)
	if got := s.View(); got == "" {
		t.Fatal("expected non-empty view even with nil workspace")
	}
}
