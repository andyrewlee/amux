package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

func TestBuildSidebarSessionAttachInfosScopesToCurrentInstance(t *testing.T) {
	sessions := []sidebarSessionInfo{
		{name: "a1", instanceID: "a", createdAt: 100},
		{name: "a2", instanceID: "a", createdAt: 101},
		{name: "b1", instanceID: "b", createdAt: 200},
	}
	out := buildSidebarSessionAttachInfos(sessions, "a")
	if len(out) != 2 {
		t.Fatalf("expected 2 sessions for instance a, got %d", len(out))
	}
	if out[0].Name != "a1" || out[1].Name != "a2" {
		t.Fatalf("expected instance a sessions, got %+v", out)
	}
}

func TestBuildSidebarSessionAttachInfosReturnsEmptyWhenNoInstanceMatch(t *testing.T) {
	sessions := []sidebarSessionInfo{
		{name: "a1", instanceID: "a", createdAt: 100},
		{name: "b1", instanceID: "b", createdAt: 200},
	}
	out := buildSidebarSessionAttachInfos(sessions, "c")
	if len(out) != 0 {
		t.Fatalf("expected no sessions for missing instance, got %+v", out)
	}
}

func TestBuildSidebarSessionAttachInfosUsesOnlyEmptyInstanceWhenUnset(t *testing.T) {
	sessions := []sidebarSessionInfo{
		{name: "x1", instanceID: "", createdAt: 100},
		{name: "a1", instanceID: "a", createdAt: 200},
	}
	out := buildSidebarSessionAttachInfos(sessions, "")
	if len(out) != 1 || out[0].Name != "x1" {
		t.Fatalf("expected only empty-instance sessions, got %+v", out)
	}
}

func TestBuildSidebarSessionAttachInfosOrdersByCreatedAt(t *testing.T) {
	sessions := []sidebarSessionInfo{
		{name: "s2", instanceID: "a", createdAt: 200},
		{name: "s1", instanceID: "a", createdAt: 100},
	}
	out := buildSidebarSessionAttachInfos(sessions, "a")
	if len(out) != 2 || out[0].Name != "s1" || out[1].Name != "s2" {
		t.Fatalf("expected created-at ordering, got %+v", out)
	}
}

func TestHandleTmuxSidebarDiscoverResultCreatesTerminalWhenEmpty(t *testing.T) {
	app := &App{}
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	app.projects = []data.Project{{Name: "p", Path: ws.Repo, Workspaces: []data.Workspace{*ws}}}
	app.sidebarTerminal = sidebar.NewTerminalModel()
	app.activeWorkspace = ws

	cmds := app.handleTmuxSidebarDiscoverResult(tmuxSidebarDiscoverResult{
		WorkspaceID: string(ws.ID()),
		Sessions:    nil,
	})
	if len(cmds) != 1 {
		t.Fatalf("expected a command to create a terminal, got %d", len(cmds))
	}
}

func TestDiscoverSidebarAttachFlags(t *testing.T) {
	sessions := []sidebarSessionInfo{
		{name: "a1", instanceID: "a", createdAt: 100, hasClients: true},
		{name: "a2", instanceID: "a", createdAt: 101, hasClients: false},
		{name: "b1", instanceID: "b", createdAt: 200, hasClients: false},
	}
	out := buildSidebarSessionAttachInfos(sessions, "a")
	if len(out) != 2 {
		t.Fatalf("expected 2 sessions for instance a, got %d", len(out))
	}
	for _, sess := range out {
		if sess.Name == "a1" && sess.DetachExisting {
			t.Fatalf("expected a1 to attach without detaching")
		}
		if sess.Name == "a2" && !sess.DetachExisting {
			t.Fatalf("expected a2 to attach with detach")
		}
		if !sess.Attach {
			t.Fatalf("expected %s to attach", sess.Name)
		}
	}
}
