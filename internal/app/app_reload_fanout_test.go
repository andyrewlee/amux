package app

import (
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// TestProjectsLoaded_SingleGitStatusBatch pins plan 041's reload contract:
// one status-refresh Cmd per reload emitting ONE GitStatusBatchResult for
// all workspaces — never N per-workspace messages/frame invalidations.
func TestProjectsLoaded_SingleGitStatusBatch(t *testing.T) {
	project := data.NewProject("/repo")
	for _, name := range []string{"a", "b", "c"} {
		ws := data.NewWorkspace(name, name, "main", "/repo", "/repo/"+name)
		project.Workspaces = append(project.Workspaces, *ws)
	}
	app := &App{
		dashboard:       dashboard.New(),
		center:          center.New(nil),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
		gitStatus:       &fileWatcherGitStatusStub{},
		lifecycle:       newWorkspaceLifecycleState(),
		tmuxActivity:    newTmuxActivityState(),
	}

	cmds := app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*project}, LoadToken: 1})

	var batchCmds, singularCmds int
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		switch msg := cmd().(type) {
		case messages.GitStatusBatchResult:
			batchCmds++
			if len(msg.Results) != 3 {
				t.Fatalf("batch results = %d, want 3 (one per workspace root)", len(msg.Results))
			}
			seen := map[string]bool{}
			for _, r := range msg.Results {
				seen[r.Root] = true
			}
			for _, name := range []string{"a", "b", "c"} {
				if !seen["/repo/"+name] {
					t.Fatalf("batch missing root /repo/%s: %v", name, seen)
				}
			}
		case messages.GitStatusResult:
			singularCmds++
		}
	}
	if singularCmds != 0 {
		t.Fatalf("reload must not emit per-workspace GitStatusResult cmds, got %d", singularCmds)
	}
	if batchCmds != 1 {
		t.Fatalf("expected exactly one GitStatusBatchResult cmd, got %d", batchCmds)
	}
}

// TestProjectsLoaded_EmitsNoBatchWithoutWorkspaces keeps the empty-load path
// from emitting a pointless batch cmd.
func TestProjectsLoaded_EmitsNoBatchWithoutWorkspaces(t *testing.T) {
	app := &App{
		dashboard:    dashboard.New(),
		center:       center.New(nil),
		gitStatus:    &fileWatcherGitStatusStub{},
		lifecycle:    newWorkspaceLifecycleState(),
		tmuxActivity: newTmuxActivityState(),
	}
	cmds := app.handleProjectsLoaded(messages.ProjectsLoaded{
		Projects:  []data.Project{*data.NewProject("/repo")},
		LoadToken: 1,
	})
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		if _, ok := cmd().(messages.GitStatusBatchResult); ok {
			t.Fatal("empty project load must not emit a status batch")
		}
	}
}

// TestHandleGitStatusBatchResult_AppliesActiveStatus: the active workspace's
// batched result reaches the sidebar changes view — observable through the
// rendered change list.
func TestHandleGitStatusBatchResult_AppliesActiveStatus(t *testing.T) {
	active := data.NewWorkspace("a", "a", "main", "/repo", "/repo/a")
	sb := sidebar.NewTabbedSidebar()
	sb.SetSize(60, 20)
	sb.SetWorkspace(active)
	app := &App{
		dashboard:       dashboard.New(),
		sidebar:         sb,
		activeWorkspace: active,
	}
	app.handleGitStatusBatchResult(messages.GitStatusBatchResult{Results: []messages.GitStatusResult{
		{Root: "/repo/a", Status: &git.StatusResult{Unstaged: []git.Change{{Path: "dirty.go", Kind: git.ChangeModified}}}},
		{Root: "/repo/b", Status: &git.StatusResult{Clean: true}},
	}})
	if view := sb.View(); !strings.Contains(view, "dirty.go") {
		t.Fatalf("sidebar must render the active workspace's batched status; got:\n%s", view)
	}
}
