package dashboard

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// TestContentVersion_MutationsBump pins the dirty-mark coverage the
// compose-time gate in internal/app relies on: every mutation that can
// change View output must move ContentVersion, or the gate would reuse a
// stale pane.
func TestContentVersion_MutationsBump(t *testing.T) {
	newModel := func() *Model {
		m := New()
		m.SetSize(80, 40)
		m.SetProjects([]data.Project{makeProject()})
		m.Focus()
		return m
	}

	t.Run("setters", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(m *Model)
		}{
			{"SetActiveWorkspaces", func(m *Model) { m.SetActiveWorkspaces(map[string]bool{"x": true}) }},
			{"SetAgentStates", func(m *Model) { m.SetAgentStates(map[string]data.AgentState{"x": data.StateWorking}) }},
			{"InvalidateStatus", func(m *Model) {
				// A dirty cached status is sticky (InvalidateStatus keeps it),
				// so the deletion path — and its version bump — needs a clean
				// cached entry.
				m.statusCache["/x"] = &git.StatusResult{Clean: true}
				m.InvalidateStatus("/x")
			}},
			{"SetCanFocusRight", func(m *Model) { m.SetCanFocusRight(true) }},
			{"SetShowKeymapHints", func(m *Model) { m.SetShowKeymapHints(true) }},
			{"SetStyles", func(m *Model) { m.SetStyles(common.DefaultStyles()) }},
			{"SetSize", func(m *Model) { m.SetSize(100, 40) }},
			{"Blur", func(m *Model) { m.Blur() }},
			{"Focus", func(m *Model) { m.Blur(); m.Focus() }},
			{"SetProjects", func(m *Model) { m.SetProjects([]data.Project{makeProject(), makeProject()}) }},
			{"SelectWorkspace", func(m *Model) { m.SelectWorkspace("ws-x") }},
			{"MarkWorkspaceIDs", func(m *Model) { m.MarkWorkspaceIDs([]string{"ws-x"}) }},
			{"ClearMarks", func(m *Model) {
				m.MarkWorkspaceIDs([]string{"ws-x"})
				v := m.ContentVersion()
				m.ClearMarks([]string{"ws-x"})
				if m.ContentVersion() == v {
					t.Fatal("ClearMarks did not bump content version")
				}
			}},
			{"ClearActiveRoot", func(m *Model) { m.ClearActiveRoot() }},
			{"SetWorkspaceBusy", func(m *Model) { m.SetWorkspaceBusy("/x", WorkspaceOpDelete, true) }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				m := newModel()
				v := m.ContentVersion()
				tc.mutate(m)
				if m.ContentVersion() == v {
					t.Fatalf("%s did not bump content version", tc.name)
				}
			})
		}
	})

	t.Run("SetWorkspaceCreating", func(t *testing.T) {
		m := newModel()
		v := m.ContentVersion()
		m.SetWorkspaceCreating(&data.Workspace{Name: "n", Repo: "/r", Root: "/r/w"}, true)
		if m.ContentVersion() == v {
			t.Fatal("SetWorkspaceCreating did not bump content version")
		}
	})

	t.Run("Update keypress", func(t *testing.T) {
		m := newModel()
		v := m.ContentVersion()
		m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		if m.ContentVersion() == v {
			t.Fatal("Update keypress did not bump content version")
		}
	})
}

// TestContentVersion_NoopMutationsStayClean asserts the cheap-side cases:
// marking is conditional where the same value would be re-written, so a
// repeated Focus or same-size SetSize must not invalidate the gate.
func TestContentVersion_NoopMutationsStayClean(t *testing.T) {
	m := New()
	m.SetSize(80, 40)
	m.Focus()
	v := m.ContentVersion()

	m.Focus()         // already focused: no-op
	m.SetSize(80, 40) // same dims: no-op
	if got := m.ContentVersion(); got != v {
		t.Fatalf("no-op mutations moved the content version: %d -> %d", v, got)
	}

	m.Blur()  // real transition: bumps
	m.Focus() // real transition: bumps
	if got := m.ContentVersion(); got != v+2 {
		t.Fatalf("focus transitions should bump exactly once each: %d -> %d", v, got)
	}
}
