package sidebar

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func versionTestWorkspace() *data.Workspace {
	return &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
}

// TestChangesContentVersion_MutationsBump pins the dirty-mark coverage the
// compose-time content gate relies on for the Changes view.
func TestChangesContentVersion_MutationsBump(t *testing.T) {
	newModel := func() *ChangesModel {
		m := NewChangesModel()
		m.SetSize(40, 20)
		m.SetWorkspace(versionTestWorkspace())
		m.Focus()
		return m
	}

	cases := []struct {
		name   string
		mutate func(m *ChangesModel)
	}{
		{"SetGitStatus", func(m *ChangesModel) {
			m.SetGitStatus(&git.StatusResult{Unstaged: []git.Change{{Path: "f.go", Kind: git.ChangeModified}}})
		}},
		{"SetScriptRunning", func(m *ChangesModel) { m.SetScriptRunning("/repo/ws", true) }},
		{"SetShowKeymapHints", func(m *ChangesModel) { m.SetShowKeymapHints(true) }},
		{"SetStyles", func(m *ChangesModel) { m.SetStyles(common.DefaultStyles()) }},
		{"SetSize", func(m *ChangesModel) { m.SetSize(60, 20) }},
		{"Blur", func(m *ChangesModel) { m.Blur() }},
		{"SetWorkspace", func(m *ChangesModel) {
			m.SetWorkspace(&data.Workspace{Name: "other", Repo: "/repo", Root: "/repo/other"})
		}},
		{"Update keypress", func(m *ChangesModel) {
			m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		}},
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
}

// TestProjectTreeContentVersion_MutationsBump does the same for the Project
// tab's tree view.
func TestProjectTreeContentVersion_MutationsBump(t *testing.T) {
	newModel := func() *ProjectTree {
		m := NewProjectTree()
		m.SetSize(40, 20)
		m.SetWorkspace(versionTestWorkspace())
		m.Focus()
		return m
	}

	cases := []struct {
		name   string
		mutate func(m *ProjectTree)
	}{
		{"SetShowKeymapHints", func(m *ProjectTree) { m.SetShowKeymapHints(true) }},
		{"SetStyles", func(m *ProjectTree) { m.SetStyles(common.DefaultStyles()) }},
		{"SetSize", func(m *ProjectTree) { m.SetSize(60, 20) }},
		{"Blur", func(m *ProjectTree) { m.Blur() }},
		{"SetWorkspace", func(m *ProjectTree) {
			m.SetWorkspace(&data.Workspace{Name: "other", Repo: "/repo", Root: "/repo/other"})
		}},
		{"Update keypress", func(m *ProjectTree) {
			m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		}},
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
}

// TestTabbedSidebarContentVersion asserts the fold tracks the active tab and
// each child's own version, and that ContentView build counting works.
func TestTabbedSidebarContentVersion(t *testing.T) {
	m := NewTabbedSidebar()
	m.SetSize(40, 20)
	m.SetWorkspace(versionTestWorkspace())

	v := m.ContentVersion()
	m.SetActiveTab(TabProject)
	if m.ContentVersion() == v {
		t.Fatal("tab switch did not move ContentVersion")
	}

	v = m.ContentVersion()
	m.changes.SetGitStatus(&git.StatusResult{Unstaged: []git.Change{{Path: "f.go"}}})
	if m.ContentVersion() == v {
		t.Fatal("changes-model mutation did not move folded ContentVersion")
	}

	v = m.ContentVersion()
	m.projectTree.SetSize(41, 20)
	if m.ContentVersion() == v {
		t.Fatal("project-tree mutation did not move folded ContentVersion")
	}

	if got := m.ContentBuildCount(); got != 0 {
		t.Fatalf("ContentBuildCount before any ContentView: %d", got)
	}
	m.ContentView()
	if got := m.ContentBuildCount(); got != 1 {
		t.Fatalf("ContentBuildCount after one ContentView: %d", got)
	}
}

// TestTerminalChromeVersions pin the fingerprint coverage for the sidebar
// terminal's gated builders: tab bar, status line, help lines.
func TestTerminalChromeVersions(t *testing.T) {
	newModel := func() *TerminalModel {
		m := NewTerminalModel()
		m.width = 80
		m.height = 30
		m.AddTerminalForHarness(versionTestWorkspace())
		return m
	}

	t.Run("tab bar", func(t *testing.T) {
		m := newModel()
		v := m.TabBarVersion()
		if m.TabBarVersion() != v {
			t.Fatal("TabBarVersion not stable across identical reads")
		}
		m.getTabs()[0].State.mu.Lock()
		m.getTabs()[0].State.Detached = true
		m.getTabs()[0].State.mu.Unlock()
		if m.TabBarVersion() == v {
			t.Fatal("detach did not move TabBarVersion")
		}
	})

	t.Run("status line", func(t *testing.T) {
		m := newModel()
		v := m.StatusLineVersion()
		if m.StatusLineVersion() != v {
			t.Fatal("StatusLineVersion not stable across identical reads")
		}
		m.DetachActiveTab()
		if m.StatusLineVersion() == v {
			t.Fatal("DetachActiveTab did not move StatusLineVersion")
		}
	})

	t.Run("status line scroll", func(t *testing.T) {
		m := newModel()
		// Fill past the viewport so scrollback exists for the scroll to move.
		m.WriteToTerminal([]byte(strings.Repeat("line\n", 200)))
		v := m.StatusLineVersion()
		ts := m.getTerminal()
		ts.mu.Lock()
		ts.VTerm.ScrollViewToTop()
		ts.mu.Unlock()
		if m.StatusLineVersion() == v {
			t.Fatal("scroll did not move StatusLineVersion")
		}
	})

	t.Run("help lines", func(t *testing.T) {
		m := newModel()
		v := m.HelpVersion()
		if m.HelpVersion() != v {
			t.Fatal("HelpVersion not stable across identical reads")
		}
		m.SetShowKeymapHints(true)
		if m.HelpVersion() == v {
			t.Fatal("SetShowKeymapHints did not move HelpVersion")
		}
		v = m.HelpVersion()
		m.SetStyles(common.DefaultStyles())
		if m.HelpVersion() == v {
			t.Fatal("SetStyles did not move HelpVersion")
		}
	})
}
