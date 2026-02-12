package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

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

func TestBuildSidebarSessionAttachInfosIncludesSessionsAcrossInstances(t *testing.T) {
	sessions := []sidebarSessionInfo{
		{name: "a1", instanceID: "inst-a", createdAt: 100},
		{name: "b1", instanceID: "inst-b", createdAt: 200},
		{name: "c1", instanceID: "inst-c", createdAt: 300},
	}
	out := buildSidebarSessionAttachInfos(sessions)
	if len(out) != 3 {
		t.Fatalf("expected 3 sessions across all instances, got %d", len(out))
	}
	names := make(map[string]bool)
	for _, s := range out {
		names[s.Name] = true
	}
	for _, expected := range []string{"a1", "b1", "c1"} {
		if !names[expected] {
			t.Fatalf("expected session %s in output", expected)
		}
	}
}

func TestBuildSidebarSessionAttachInfosHandlesEmpty(t *testing.T) {
	out := buildSidebarSessionAttachInfos(nil)
	if len(out) != 0 {
		t.Fatalf("expected empty output for nil input, got %d", len(out))
	}

	out = buildSidebarSessionAttachInfos([]sidebarSessionInfo{})
	if len(out) != 0 {
		t.Fatalf("expected empty output for empty input, got %d", len(out))
	}
}

func TestBuildSidebarSessionAttachInfosOrdersByCreatedAt(t *testing.T) {
	sessions := []sidebarSessionInfo{
		{name: "s3", instanceID: "a", createdAt: 300},
		{name: "s1", instanceID: "b", createdAt: 100},
		{name: "s2", instanceID: "c", createdAt: 200},
	}
	out := buildSidebarSessionAttachInfos(sessions)
	if len(out) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(out))
	}
	expected := []string{"s1", "s2", "s3"}
	for i, name := range expected {
		if out[i].Name != name {
			t.Fatalf("position %d: expected %s, got %s", i, name, out[i].Name)
		}
	}
}

func TestDiscoverSidebarAttachFlags(t *testing.T) {
	sessions := []sidebarSessionInfo{
		{name: "a1", instanceID: "a", createdAt: 100, hasClients: true},
		{name: "a2", instanceID: "b", createdAt: 101, hasClients: false},
		{name: "b1", instanceID: "c", createdAt: 200, hasClients: false},
	}
	out := buildSidebarSessionAttachInfos(sessions)
	if len(out) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(out))
	}
	for _, sess := range out {
		if !sess.Attach {
			t.Fatalf("expected %s to have Attach=true", sess.Name)
		}
		switch sess.Name {
		case "a1":
			if sess.DetachExisting {
				t.Fatal("expected a1 to attach without detaching (has clients)")
			}
		case "a2", "b1":
			if !sess.DetachExisting {
				t.Fatalf("expected %s to attach with detach (no clients)", sess.Name)
			}
		}
	}
}
