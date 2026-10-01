package app

import (
	"errors"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/testutil/tmuxops"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// newDiscoverTestApp wires the minimal App a sidebar-discover result handler
// needs: the workspace registered in projects, an active workspace pointer,
// and a real sidebar terminal model so a wrongful auto-create is observable.
func newDiscoverTestApp(t *testing.T) (*App, *data.Workspace) {
	t.Helper()
	app := &App{}
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	app.projects = []data.Project{{Name: "p", Path: ws.Repo, Workspaces: []data.Workspace{*ws}}}
	app.sidebarTerminal = sidebar.NewTerminalModel()
	app.activeWorkspace = ws
	return app, ws
}

func TestHandleTmuxSidebarDiscoverResultCreatesTerminalWhenEmpty(t *testing.T) {
	app, ws := newDiscoverTestApp(t)

	cmds := app.handleTmuxSidebarDiscoverResult(tmuxSidebarDiscoverResult{
		WorkspaceID: string(ws.ID()),
		Sessions:    nil,
	})
	if len(cmds) != 1 {
		t.Fatalf("expected a command to create a terminal, got %d", len(cmds))
	}
}

// TestHandleTmuxSidebarDiscoverResultSkipsCreateOnError proves a failed
// listing does not manufacture a terminal tab: Err means "unknown", not
// "empty", so the handler defers to the next sync tick.
func TestHandleTmuxSidebarDiscoverResultSkipsCreateOnError(t *testing.T) {
	app, ws := newDiscoverTestApp(t)
	before := app.sidebarTerminal.TabBarVersion()

	cmds := app.handleTmuxSidebarDiscoverResult(tmuxSidebarDiscoverResult{
		WorkspaceID: string(ws.ID()),
		Err:         errors.New("list-sessions failed"),
	})
	if len(cmds) != 0 {
		t.Fatalf("expected no commands on a failed discovery, got %d", len(cmds))
	}
	if got := app.sidebarTerminal.TabBarVersion(); got != before {
		t.Fatal("failed discovery must not touch the sidebar terminal model")
	}
}

// TestDiscoverSidebarTerminalsCarriesListError proves the producer sets Err
// when the session listing itself fails.
func TestDiscoverSidebarTerminalsCarriesListError(t *testing.T) {
	app, ws := newDiscoverTestApp(t)
	app.tmuxAvailable = true
	app.tmuxService = &tmuxops.FakeTmuxOps{
		SessionsWithTagsFunc: func(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
			return nil, errors.New("server unavailable")
		},
	}

	cmd := app.discoverSidebarTerminalsFromTmux(ws)
	if cmd == nil {
		t.Fatal("expected a discover cmd")
	}
	res, ok := cmd().(tmuxSidebarDiscoverResult)
	if !ok {
		t.Fatalf("expected tmuxSidebarDiscoverResult, got %T", cmd())
	}
	if res.Err == nil {
		t.Fatal("expected Err set when the session listing fails")
	}
}

// TestDiscoverSidebarTerminalsFlagsDegradedEmpty proves that rows which all
// dropped out while a metadata batch failed are flagged, not reported as a
// genuine empty.
func TestDiscoverSidebarTerminalsFlagsDegradedEmpty(t *testing.T) {
	app, ws := newDiscoverTestApp(t)
	app.tmuxAvailable = true
	app.tmuxService = &tmuxops.FakeTmuxOps{
		SessionsWithTagsFunc: func(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
			return []tmux.SessionTagValues{
				{Name: "amux-1", Tags: map[string]string{"@amux_workspace": string(ws.ID())}},
			}, nil
		},
		AllSessionStatesFunc: func(tmux.Options) (map[string]tmux.SessionState, error) {
			return nil, errors.New("states unavailable")
		},
	}

	cmd := app.discoverSidebarTerminalsFromTmux(ws)
	res, ok := cmd().(tmuxSidebarDiscoverResult)
	if !ok {
		t.Fatalf("expected tmuxSidebarDiscoverResult, got %T", cmd())
	}
	if res.Err == nil {
		t.Fatal("expected Err set when a batch failure emptied a non-empty row list")
	}
	if len(res.Sessions) != 0 {
		t.Fatalf("expected no sessions on a degraded listing, got %d", len(res.Sessions))
	}
}

// TestDiscoverSidebarTerminalsGenuineEmptyStaysTrusted proves that a listing
// with no rows and healthy batches reports empty WITHOUT Err — auto-create
// remains correct then.
func TestDiscoverSidebarTerminalsGenuineEmptyStaysTrusted(t *testing.T) {
	app, ws := newDiscoverTestApp(t)
	app.tmuxAvailable = true
	app.tmuxService = &tmuxops.FakeTmuxOps{
		SessionsWithTagsFunc: func(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
			return nil, nil
		},
		AllSessionStatesFunc: func(tmux.Options) (map[string]tmux.SessionState, error) {
			return map[string]tmux.SessionState{}, nil
		},
	}

	cmd := app.discoverSidebarTerminalsFromTmux(ws)
	res, ok := cmd().(tmuxSidebarDiscoverResult)
	if !ok {
		t.Fatalf("expected tmuxSidebarDiscoverResult, got %T", cmd())
	}
	if res.Err != nil {
		t.Fatalf("genuine empty must not carry Err, got %v", res.Err)
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
