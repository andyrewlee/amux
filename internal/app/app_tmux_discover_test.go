package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

func TestSelectSidebarInstancePrefersCurrentInstance(t *testing.T) {
	sessions := []sidebarSessionInfo{
		{name: "a1", instanceID: "a", createdAt: 100},
		{name: "a2", instanceID: "a", createdAt: 101},
		{name: "b1", instanceID: "b", createdAt: 200},
	}
	out := selectSidebarInstance(sessions, "a")
	if !out.OK || out.ID != "a" {
		t.Fatalf("expected instance a, got %#v", out)
	}
}

func TestSelectSidebarInstanceFallsBackToLargestInstance(t *testing.T) {
	sessions := []sidebarSessionInfo{
		{name: "a1", instanceID: "a", createdAt: 300},
		{name: "b1", instanceID: "b", createdAt: 200},
		{name: "b2", instanceID: "b", createdAt: 201},
	}
	out := selectSidebarInstance(sessions, "c")
	if !out.OK || out.ID != "b" {
		t.Fatalf("expected instance b by count, got %#v", out)
	}
}

func TestSelectSidebarInstanceNoInstanceTags(t *testing.T) {
	sessions := []sidebarSessionInfo{
		{name: "x1", instanceID: "", createdAt: 0},
		{name: "x2", instanceID: "", createdAt: 0},
	}
	out := selectSidebarInstance(sessions, "a")
	if !out.OK || out.ID != "" {
		t.Fatalf("expected empty instance selection, got %#v", out)
	}
}

func TestSelectSidebarInstanceUsesCountOverRecency(t *testing.T) {
	sessions := []sidebarSessionInfo{
		{name: "a1", instanceID: "a", createdAt: 300},
		{name: "b1", instanceID: "b", createdAt: 200},
		{name: "b2", instanceID: "b", createdAt: 201},
	}
	out := selectSidebarInstance(sessions, "")
	if !out.OK || out.ID != "b" {
		t.Fatalf("expected instance b by count, got %#v", out)
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

func TestDiscoverSidebarSessionsOrdersByCreatedAt(t *testing.T) {
	sessions := []sidebarSessionInfo{
		{name: "s2", instanceID: "a", createdAt: 200},
		{name: "s1", instanceID: "a", createdAt: 100},
	}
	chosen := selectSidebarInstance(sessions, "a")
	if !chosen.OK || chosen.ID != "a" {
		t.Fatalf("expected instance a, got %#v", chosen)
	}
	out := buildSidebarSessionAttachInfos(sessions, chosen)
	if len(out) != 2 || out[0].Name != "s1" || out[1].Name != "s2" {
		t.Fatalf("expected created-at ordering, got %v", []string{out[0].Name, out[1].Name})
	}
}

func TestDiscoverSidebarAttachFlags(t *testing.T) {
	sessions := []sidebarSessionInfo{
		{name: "a1", instanceID: "a", createdAt: 100, hasClients: true},
		{name: "a2", instanceID: "a", createdAt: 101, hasClients: false},
		{name: "b1", instanceID: "b", createdAt: 200, hasClients: false},
	}
	chosen := selectSidebarInstance(sessions, "a")
	out := buildSidebarSessionAttachInfos(sessions, chosen)
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
