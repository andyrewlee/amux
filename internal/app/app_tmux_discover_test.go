package app

import (
	"sort"
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

func TestFilterSessionsWithoutClients(t *testing.T) {
	sessions := []sidebarSessionInfo{
		{name: "a"},
		{name: "b", hasClients: true},
		{name: ""},
	}
	out := filterSessionsWithoutClients(sessions)
	if len(out) != 1 || out[0].name != "a" {
		t.Fatalf("expected only session a, got %v", out)
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
	chosenSessions := make([]sidebarSessionInfo, 0, len(sessions))
	for _, session := range sessions {
		if session.instanceID != chosen.ID {
			continue
		}
		chosenSessions = append(chosenSessions, session)
	}
	sort.SliceStable(chosenSessions, func(i, j int) bool {
		ci, cj := chosenSessions[i].createdAt, chosenSessions[j].createdAt
		if ci != 0 || cj != 0 {
			if ci == 0 {
				return false
			}
			if cj == 0 {
				return true
			}
			if ci != cj {
				return ci < cj
			}
		}
		return chosenSessions[i].name < chosenSessions[j].name
	})
	if chosenSessions[0].name != "s1" || chosenSessions[1].name != "s2" {
		t.Fatalf("expected created-at ordering, got %v", []string{chosenSessions[0].name, chosenSessions[1].name})
	}
}
