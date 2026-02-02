package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

func TestSelectSidebarSessionsPrefersCurrentInstance(t *testing.T) {
	sessions := []sidebarSessionInfo{
		{name: "a1", instanceID: "a", createdAt: 100},
		{name: "a2", instanceID: "a", createdAt: 101},
		{name: "b1", instanceID: "b", createdAt: 200},
	}
	latest := map[string]int64{"a": 101, "b": 200}
	out := selectSidebarSessions(sessions, latest, "a")
	if len(out) != 2 {
		t.Fatalf("expected 2 sessions, got %v", out)
	}
	for _, name := range out {
		if name != "a1" && name != "a2" {
			t.Fatalf("unexpected session %s", name)
		}
	}
}

func TestSelectSidebarSessionsFallsBackToLatestInstance(t *testing.T) {
	sessions := []sidebarSessionInfo{
		{name: "a1", instanceID: "a", createdAt: 100},
		{name: "b1", instanceID: "b", createdAt: 200},
	}
	latest := map[string]int64{"a": 100, "b": 200}
	out := selectSidebarSessions(sessions, latest, "c")
	if len(out) != 1 || out[0] != "b1" {
		t.Fatalf("expected latest instance sessions, got %v", out)
	}
}

func TestSelectSidebarSessionsNoInstanceTags(t *testing.T) {
	sessions := []sidebarSessionInfo{
		{name: "x1", instanceID: "", createdAt: 0},
		{name: "x2", instanceID: "", createdAt: 0},
	}
	out := selectSidebarSessions(sessions, map[string]int64{}, "a")
	if len(out) != 2 {
		t.Fatalf("expected all sessions when no instance tags, got %v", out)
	}
}

func TestFilterSessionsWithoutClients(t *testing.T) {
	sessions := []sidebarSessionInfo{
		{name: "a"},
		{name: "b"},
		{name: ""},
	}
	hasClients := map[string]bool{"b": true}
	out := filterSessionsWithoutClients(sessions, hasClients)
	if len(out) != 1 || out[0].name != "a" {
		t.Fatalf("expected only session a, got %v", out)
	}
}

func TestHandleTmuxSidebarDiscoverResultCreatesTerminalWhenEmpty(t *testing.T) {
	app := &App{}
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	app.projects = []data.Project{{Name: "p", Path: ws.Repo, Workspaces: []data.Workspace{*ws}}}
	app.sidebarTerminal = sidebar.NewTerminalModel()

	cmds := app.handleTmuxSidebarDiscoverResult(tmuxSidebarDiscoverResult{
		WorkspaceID: string(ws.ID()),
		Sessions:    nil,
	})
	if len(cmds) != 1 {
		t.Fatalf("expected a command to create a terminal, got %d", len(cmds))
	}
}
