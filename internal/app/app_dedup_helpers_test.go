package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/layout"
)

// appWithWorkspaces builds an App whose projects hold workspaces with the given
// roots. Distinct roots yield distinct workspace IDs (ID derives from repo+root).
func appWithWorkspaces(projectRoots ...[]string) *App {
	a := &App{}
	for _, roots := range projectRoots {
		project := data.Project{Name: "p", Path: "/repo"}
		for _, root := range roots {
			project.Workspaces = append(project.Workspaces, data.Workspace{
				Name: root,
				Repo: "/repo",
				Root: root,
			})
		}
		a.projects = append(a.projects, project)
	}
	return a
}

func TestEachWorkspace_VisitsEveryWorkspaceInOrder(t *testing.T) {
	a := appWithWorkspaces(
		[]string{"/ws/a", "/ws/b"},
		[]string{"/ws/c"},
	)

	var visited []string
	var projectsSeen []string
	a.eachWorkspace(func(ws *data.Workspace, project *data.Project) {
		visited = append(visited, ws.Root)
		projectsSeen = append(projectsSeen, project.Path)
	})

	want := []string{"/ws/a", "/ws/b", "/ws/c"}
	if len(visited) != len(want) {
		t.Fatalf("visited %v, want %v", visited, want)
	}
	for i := range want {
		if visited[i] != want[i] {
			t.Fatalf("visited[%d] = %q, want %q (full: %v)", i, visited[i], want[i], visited)
		}
		if projectsSeen[i] != "/repo" {
			t.Fatalf("projectsSeen[%d] = %q, want /repo", i, projectsSeen[i])
		}
	}
}

func TestEachWorkspace_MutatesInPlace(t *testing.T) {
	a := appWithWorkspaces([]string{"/ws/a", "/ws/b"})

	a.eachWorkspace(func(ws *data.Workspace, _ *data.Project) {
		ws.Branch = "feature/" + ws.Root
	})

	if got := a.projects[0].Workspaces[0].Branch; got != "feature//ws/a" {
		t.Fatalf("first workspace branch = %q, want feature//ws/a", got)
	}
	if got := a.projects[0].Workspaces[1].Branch; got != "feature//ws/b" {
		t.Fatalf("second workspace branch = %q, want feature//ws/b", got)
	}
}

func TestEachWorkspaceUntil_StopsOnFirstMatch(t *testing.T) {
	a := appWithWorkspaces(
		[]string{"/ws/a", "/ws/b"},
		[]string{"/ws/c"},
	)

	var visited []string
	stopped := a.eachWorkspaceUntil(func(ws *data.Workspace, _ *data.Project) bool {
		visited = append(visited, ws.Root)
		return ws.Root == "/ws/b"
	})

	if !stopped {
		t.Fatal("expected eachWorkspaceUntil to report an early stop")
	}
	if len(visited) != 2 || visited[0] != "/ws/a" || visited[1] != "/ws/b" {
		t.Fatalf("expected to stop at /ws/b after visiting a,b; visited %v", visited)
	}
}

func TestEachWorkspaceUntil_NoMatchVisitsAllAndReturnsFalse(t *testing.T) {
	a := appWithWorkspaces([]string{"/ws/a"}, []string{"/ws/b"})

	count := 0
	stopped := a.eachWorkspaceUntil(func(_ *data.Workspace, _ *data.Project) bool {
		count++
		return false
	})

	if stopped {
		t.Fatal("expected no early stop")
	}
	if count != 2 {
		t.Fatalf("expected to visit 2 workspaces, visited %d", count)
	}
}

// TestCenterButtons_CountActivateRenderParity pins that the count, activation,
// and label-rendering paths all agree with the single centerButtonsFor source.
func TestCenterButtons_CountActivateRenderParity(t *testing.T) {
	cases := []struct {
		name       string
		state      centerButtonState
		wantLabels []string
		wantMsgs   []tea.Msg
	}{
		{
			name:       "welcome",
			state:      centerButtonsWelcome,
			wantLabels: []string{"[Add project]", "[Settings]"},
			wantMsgs:   []tea.Msg{messages.ShowAddProjectDialog{}, messages.ShowSettingsDialog{}},
		},
		{
			name:       "workspace",
			state:      centerButtonsWorkspace,
			wantLabels: []string{"[New agent]"},
			wantMsgs:   []tea.Msg{messages.ShowSelectAssistantDialog{}},
		},
		{
			name:       "none",
			state:      centerButtonsNone,
			wantLabels: nil,
			wantMsgs:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buttons := centerButtonsFor(tc.state)
			if len(buttons) != len(tc.wantLabels) {
				t.Fatalf("len(buttons) = %d, want %d", len(buttons), len(tc.wantLabels))
			}
			for i, b := range buttons {
				if b.label != tc.wantLabels[i] {
					t.Fatalf("button[%d].label = %q, want %q", i, b.label, tc.wantLabels[i])
				}
				if b.msg != tc.wantMsgs[i] {
					t.Fatalf("button[%d].msg = %T, want %T", i, b.msg, tc.wantMsgs[i])
				}
			}
		})
	}
}

func TestCurrentCenterButtonState_MatchesFlags(t *testing.T) {
	welcome := &App{showWelcome: true}
	if got := welcome.currentCenterButtonState(); got != centerButtonsWelcome {
		t.Fatalf("welcome state = %v, want welcome", got)
	}
	if got := welcome.centerButtonCount(); got != 2 {
		t.Fatalf("welcome count = %d, want 2", got)
	}

	ws := &App{activeWorkspace: &data.Workspace{Name: "ws"}}
	if got := ws.currentCenterButtonState(); got != centerButtonsWorkspace {
		t.Fatalf("workspace state = %v, want workspace", got)
	}
	if got := ws.centerButtonCount(); got != 1 {
		t.Fatalf("workspace count = %d, want 1", got)
	}

	none := &App{}
	if got := none.currentCenterButtonState(); got != centerButtonsNone {
		t.Fatalf("none state = %v, want none", got)
	}
	if got := none.centerButtonCount(); got != 0 {
		t.Fatalf("none count = %d, want 0", got)
	}
}

// TestAdjustMouseMsg_TranslatesPerPaneAndPreservesType pins the coordinate
// translation that dispatchToPane relies on: adjustable panes are shifted by
// the gutters while the sidebar terminal is left in screen space, and the
// concrete message type is preserved so each child's type switch still matches.
func TestAdjustMouseMsg_TranslatesPerPaneAndPreservesType(t *testing.T) {
	l := layout.NewManager()
	l.Resize(140, 40)
	a := &App{layout: l}

	left := l.LeftGutter()
	top := l.TopGutter()

	// Dashboard subtracts both gutters.
	got := a.adjustMouseMsg(messages.PaneDashboard, tea.MouseClickMsg{X: left + 5, Y: top + 3})
	click, ok := got.(tea.MouseClickMsg)
	if !ok {
		t.Fatalf("adjustMouseMsg changed type: got %T, want tea.MouseClickMsg", got)
	}
	if click.X != 5 || click.Y != 3 {
		t.Fatalf("dashboard adjust = (%d,%d), want (5,3)", click.X, click.Y)
	}

	// Center subtracts only the top gutter (X unchanged).
	wheel, ok := a.adjustMouseMsg(messages.PaneCenter, tea.MouseWheelMsg{X: 7, Y: top + 9}).(tea.MouseWheelMsg)
	if !ok {
		t.Fatal("center adjust changed message type")
	}
	if wheel.X != 7 || wheel.Y != 9 {
		t.Fatalf("center adjust = (%d,%d), want (7,9)", wheel.X, wheel.Y)
	}

	// Sidebar terminal coordinates are left untouched by adjustMouseXY's default.
	motion, ok := a.adjustMouseMsg(messages.PaneSidebarTerminal, tea.MouseMotionMsg{X: 11, Y: 13}).(tea.MouseMotionMsg)
	if !ok {
		t.Fatal("sidebar-terminal adjust changed message type")
	}
	if motion.X != 11 || motion.Y != 13 {
		t.Fatalf("sidebar-terminal adjust = (%d,%d), want (11,13)", motion.X, motion.Y)
	}
}

// TestPropagateStyles_SkipsNilFilePicker confirms the nil-guarded fan-out is
// safe to call before the file picker exists (the construction-time context).
func TestPropagateStyles_SkipsNilFilePicker(t *testing.T) {
	a := newAppShell(nil)
	if a.filePicker != nil {
		t.Fatal("expected filePicker to be nil at construction")
	}
	// Must not panic with a nil filePicker.
	a.propagateStyles()
}
