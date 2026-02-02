package app

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

// ---------------------------------------------------------------------------
// collectKnownWorkspaceIDs — pure unit tests (no tmux)
// ---------------------------------------------------------------------------

func TestCollectKnownWorkspaceIDs_Empty(t *testing.T) {
	app := &App{}
	ids := app.collectKnownWorkspaceIDs()
	if len(ids) != 0 {
		t.Fatalf("expected empty map, got %v", ids)
	}
}

func TestCollectKnownWorkspaceIDs_MultipleProjects(t *testing.T) {
	app := &App{
		projects: []data.Project{
			{
				Path: "/repo-a",
				Workspaces: []data.Workspace{
					{Repo: "/repo-a", Root: "/repo-a/ws1"},
					{Repo: "/repo-a", Root: "/repo-a/ws2"},
				},
			},
			{
				Path: "/repo-b",
				Workspaces: []data.Workspace{
					{Repo: "/repo-b", Root: "/repo-b/ws3"},
				},
			},
		},
	}

	ids := app.collectKnownWorkspaceIDs()
	if len(ids) != 3 {
		t.Fatalf("expected 3 workspace IDs, got %d: %v", len(ids), ids)
	}

	// Verify each workspace's ID is present.
	for _, p := range app.projects {
		for _, ws := range p.Workspaces {
			id := string(ws.ID())
			if !ids[id] {
				t.Errorf("missing workspace ID %s (repo=%s root=%s)", id, ws.Repo, ws.Root)
			}
		}
	}
}

func TestCollectKnownWorkspaceIDs_IncludesCreating(t *testing.T) {
	ws := data.Workspace{Repo: "/repo-c", Root: "/repo-c/ws-create"}
	creatingID := string(ws.ID())
	app := &App{
		creatingWorkspaceIDs: map[string]bool{creatingID: true},
	}

	ids := app.collectKnownWorkspaceIDs()
	if !ids[creatingID] {
		t.Fatalf("expected creating workspace ID %s to be included", creatingID)
	}
}

// ---------------------------------------------------------------------------
// gcOrphanedTmuxSessions gating — pure unit tests (no tmux)
// ---------------------------------------------------------------------------

func TestGcSkipsWhenTmuxUnavailable(t *testing.T) {
	app := &App{
		tmuxAvailable:  false,
		projectsLoaded: true,
	}
	cmd := app.gcOrphanedTmuxSessions()
	if cmd != nil {
		t.Fatal("expected nil Cmd when tmux unavailable")
	}
}

func TestGcSkipsWhenProjectsNotLoaded(t *testing.T) {
	app := &App{
		tmuxAvailable:  true,
		projectsLoaded: false,
	}
	cmd := app.gcOrphanedTmuxSessions()
	if cmd != nil {
		t.Fatal("expected nil Cmd when projects not loaded")
	}
}

func TestGcReturnsCmdWhenReady(t *testing.T) {
	app := &App{
		tmuxAvailable:  true,
		projectsLoaded: true,
	}
	cmd := app.gcOrphanedTmuxSessions()
	if cmd == nil {
		t.Fatal("expected non-nil Cmd when both flags true")
	}
}

// ---------------------------------------------------------------------------
// handleOrphanGCResult — pure unit tests
// ---------------------------------------------------------------------------

func TestHandleOrphanGCResult(t *testing.T) {
	app := &App{}

	// Should not panic for any of these cases.
	app.handleOrphanGCResult(orphanGCResult{Killed: 5})
	app.handleOrphanGCResult(orphanGCResult{Err: errors.New("boom")})
	app.handleOrphanGCResult(orphanGCResult{Killed: 0})
}

// ---------------------------------------------------------------------------
// Integration helpers (real tmux)
// ---------------------------------------------------------------------------

func skipIfNoTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
}

func gcTestServer(t *testing.T) tmux.Options {
	t.Helper()
	name := fmt.Sprintf("amux-gctest-%d", time.Now().UnixNano())
	opts := tmux.Options{
		ServerName:     name,
		ConfigPath:     "/dev/null",
		CommandTimeout: 5 * time.Second,
	}
	t.Cleanup(func() {
		cmd := exec.Command("tmux", "-L", name, "kill-server")
		_ = cmd.Run()
	})
	return opts
}

func gcCreateSession(t *testing.T, opts tmux.Options, name, command string) {
	t.Helper()
	args := gcTmuxArgs(opts, "new-session", "-d", "-s", name, "sh", "-c", command)
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("create session %q: %v\n%s", name, err, out)
	}
}

func gcSetTag(t *testing.T, opts tmux.Options, session, key, val string) {
	t.Helper()
	args := gcTmuxArgs(opts, "set-option", "-t", session, key, val)
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("set tag %s=%s on %s: %v\n%s", key, val, session, err, out)
	}
}

func gcHasSession(t *testing.T, opts tmux.Options, name string) bool {
	t.Helper()
	args := gcTmuxArgs(opts, "has-session", "-t", name)
	cmd := exec.Command("tmux", args...)
	return cmd.Run() == nil
}

func gcTmuxArgs(opts tmux.Options, args ...string) []string {
	out := []string{}
	if opts.ServerName != "" {
		out = append(out, "-L", opts.ServerName)
	}
	if opts.ConfigPath != "" {
		out = append(out, "-f", opts.ConfigPath)
	}
	out = append(out, args...)
	return out
}

// ---------------------------------------------------------------------------
// GC integration tests (real tmux)
// ---------------------------------------------------------------------------

func TestGcOrphanedTmuxSessions_Integration(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)

	// Create a "known" workspace whose ID we will register with the app.
	knownWs := data.Workspace{Repo: "/test/repo", Root: "/test/repo/known"}
	knownID := string(knownWs.ID())

	// Create 3 sessions: 1 known, 2 orphans.
	gcCreateSession(t, opts, "known-sess", "sleep 300")
	gcCreateSession(t, opts, "orphan1", "sleep 300")
	gcCreateSession(t, opts, "orphan2", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	// Tag all as @amux with different workspace IDs.
	gcSetTag(t, opts, "known-sess", "@amux", "1")
	gcSetTag(t, opts, "known-sess", "@amux_workspace", knownID)

	gcSetTag(t, opts, "orphan1", "@amux", "1")
	gcSetTag(t, opts, "orphan1", "@amux_workspace", "dead-ws-1")

	gcSetTag(t, opts, "orphan2", "@amux", "1")
	gcSetTag(t, opts, "orphan2", "@amux_workspace", "dead-ws-2")

	app := &App{
		tmuxAvailable:  true,
		projectsLoaded: true,
		tmuxOptions:    opts,
		projects: []data.Project{
			{
				Path:       "/test/repo",
				Workspaces: []data.Workspace{knownWs},
			},
		},
	}

	cmd := app.gcOrphanedTmuxSessions()
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}

	msg := cmd()
	result, ok := msg.(orphanGCResult)
	if !ok {
		t.Fatalf("expected orphanGCResult, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("GC error: %v", result.Err)
	}
	if result.Killed != 2 {
		t.Fatalf("expected 2 killed, got %d", result.Killed)
	}

	// Known session must survive.
	if !gcHasSession(t, opts, "known-sess") {
		t.Fatal("known session was killed — should have survived GC")
	}
	// Orphans must be gone.
	if gcHasSession(t, opts, "orphan1") {
		t.Fatal("orphan1 should have been killed")
	}
	if gcHasSession(t, opts, "orphan2") {
		t.Fatal("orphan2 should have been killed")
	}
}

func TestGcOrphanedTmuxSessions_NoSessions(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)

	app := &App{
		tmuxAvailable:  true,
		projectsLoaded: true,
		tmuxOptions:    opts,
	}

	cmd := app.gcOrphanedTmuxSessions()
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}

	msg := cmd()
	result, ok := msg.(orphanGCResult)
	if !ok {
		t.Fatalf("expected orphanGCResult, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("GC error: %v", result.Err)
	}
	if result.Killed != 0 {
		t.Fatalf("expected 0 killed, got %d", result.Killed)
	}
}

// ---------------------------------------------------------------------------
// Terminal GC selection — pure unit tests
// ---------------------------------------------------------------------------

func TestSelectStaleTerminalSessions(t *testing.T) {
	now := time.Unix(1000, 0)
	ttl := 10 * time.Second

	sessions := []terminalSessionInfo{
		{Name: "keep-clients", CreatedAt: 980, HasClients: true},
		{Name: "keep-recent", CreatedAt: 995, HasClients: false},
		{Name: "keep-unknown", CreatedAt: 0, HasClients: false},
		{Name: "kill-stale", CreatedAt: 990, HasClients: false},
	}

	out := selectStaleTerminalSessions(sessions, now, ttl)
	if len(out) != 1 || out[0] != "kill-stale" {
		t.Fatalf("expected only kill-stale, got %v", out)
	}
}

func TestShouldSkipTerminalForInstance(t *testing.T) {
	active := map[string]bool{
		"other": true,
	}
	if shouldSkipTerminalForInstance("other", "current", active) != true {
		t.Fatal("expected skip for active other instance")
	}
	if shouldSkipTerminalForInstance("current", "current", active) != false {
		t.Fatal("expected no skip for current instance")
	}
	if shouldSkipTerminalForInstance("", "current", active) != false {
		t.Fatal("expected no skip for empty instance")
	}
	if shouldSkipTerminalForInstance("stale", "current", active) != false {
		t.Fatal("expected no skip for inactive instance")
	}
}
