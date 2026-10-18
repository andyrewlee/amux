package dashboard

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
)

// benchRegistry builds ~10 projects × 15 workspaces each — the plan-128
// scale for the per-keystroke filter cost.
func benchRegistry() []data.Project {
	projects := make([]data.Project, 0, 10)
	for p := 0; p < 10; p++ {
		proj := data.Project{
			Name: fmt.Sprintf("project-%02d", p),
			Path: fmt.Sprintf("/repos/project-%02d", p),
		}
		proj.Workspaces = append(proj.Workspaces, data.Workspace{
			Name:   proj.Name,
			Branch: "main",
			Repo:   proj.Path,
			Root:   proj.Path,
		})
		for w := 0; w < 14; w++ {
			name := fmt.Sprintf("feat-%02d-%02d", p, w)
			proj.Workspaces = append(proj.Workspaces, data.Workspace{
				Name:   name,
				Branch: "branch-" + name,
				Repo:   proj.Path,
				Root:   proj.Path + "/.amux/ws/" + name,
			})
		}
		projects = append(projects, proj)
	}
	return projects
}

// BenchmarkDashboardFilterKeystroke measures one query edit on the active
// `/` filter — the path that used to re-flatten and re-sort the whole
// registry per keypress.
func BenchmarkDashboardFilterKeystroke(b *testing.B) {
	m := New()
	m.SetSize(80, 30)
	m.SetProjects(benchRegistry())
	m.Focus()
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	// Seed a query so each measured keypress edits a non-empty filter.
	for _, r := range "feat" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	keys := []rune{'-', '0', '1', tea.KeyBackspace}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := keys[i%len(keys)]
		var msg tea.KeyPressMsg
		if k == tea.KeyBackspace {
			msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
		} else {
			msg = tea.KeyPressMsg{Code: k, Text: string(k)}
		}
		m.Update(msg)
	}
}
