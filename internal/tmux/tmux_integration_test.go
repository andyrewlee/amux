package tmux

import (
	"fmt"
	"os/exec"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test helpers for real-tmux integration tests
// ---------------------------------------------------------------------------

func skipIfNoTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
}

func ensureTmuxServer(t *testing.T, opts Options) {
	t.Helper()
	args := tmuxArgs(opts, "start-server")
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("tmux server socket unavailable: %v\n%s", err, out)
	}
	// Verify the server is reachable.
	args = tmuxArgs(opts, "show-options", "-g")
	cmd = exec.Command("tmux", args...)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Skipf("tmux server socket unreachable: %v\n%s", err, out)
	}
}

// testServer returns Options pointing at an isolated tmux server and registers
// a cleanup that kills the server when the test finishes.
func testServer(t *testing.T) Options {
	t.Helper()
	name := fmt.Sprintf("amux-test-%d", time.Now().UnixNano())
	opts := Options{
		ServerName:     name,
		ConfigPath:     "/dev/null",
		CommandTimeout: 5 * time.Second,
	}
	t.Cleanup(func() {
		cmd := exec.Command("tmux", "-L", name, "kill-server")
		_ = cmd.Run()
	})
	ensureTmuxServer(t, opts)
	return opts
}

// createSession creates a detached tmux session running cmd.
func createSession(t *testing.T, opts Options, name, command string) {
	t.Helper()
	args := tmuxArgs(opts, "new-session", "-d", "-s", name, "sh", "-c", command)
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("create session %q: %v\n%s", name, err, out)
	}
}

// setTag sets an @-prefixed session option.
func setTag(t *testing.T, opts Options, session, key, val string) {
	t.Helper()
	args := tmuxArgs(opts, "set-option", "-t", session, key, val)
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("set tag %s=%s on %s: %v\n%s", key, val, session, err, out)
	}
}

// addWindow adds a new window to an existing session.
func addWindow(t *testing.T, opts Options, session, command string) {
	t.Helper()
	args := tmuxArgs(opts, "new-window", "-t", session, "sh", "-c", command)
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("add window to %s: %v\n%s", session, err, out)
	}
}

// ---------------------------------------------------------------------------
// PanePIDs tests
// ---------------------------------------------------------------------------

func TestPanePIDs_NonexistentSession(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	pids, err := PanePIDs("no-such-session", opts)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if pids != nil {
		t.Fatalf("expected nil pids, got %v", pids)
	}
}

func TestPanePIDs_SingleWindow(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "single", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	pids, err := PanePIDs("single", opts)
	if err != nil {
		t.Fatalf("PanePIDs: %v", err)
	}
	if len(pids) != 1 {
		t.Fatalf("expected 1 PID, got %d: %v", len(pids), pids)
	}
	if pids[0] <= 0 {
		t.Fatalf("expected PID > 0, got %d", pids[0])
	}
}

func TestPanePIDs_MultipleWindows(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "multi", "sleep 300")
	addWindow(t, opts, "multi", "sleep 300")
	addWindow(t, opts, "multi", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	pids, err := PanePIDs("multi", opts)
	if err != nil {
		t.Fatalf("PanePIDs: %v", err)
	}
	if len(pids) != 3 {
		t.Fatalf("expected 3 PIDs (regression: -s flag), got %d: %v", len(pids), pids)
	}
	seen := make(map[int]bool)
	for _, pid := range pids {
		if pid <= 0 {
			t.Fatalf("expected PID > 0, got %d", pid)
		}
		if seen[pid] {
			t.Fatalf("duplicate PID %d", pid)
		}
		seen[pid] = true
	}
}

// ---------------------------------------------------------------------------
// KillSession tests (non-process-tree)
// ---------------------------------------------------------------------------

func TestKillSession_EmptyName(t *testing.T) {
	err := KillSession("", Options{})
	if err != nil {
		t.Fatalf("expected nil for empty name, got %v", err)
	}
}

func TestKillSession_NonexistentSession(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	err := KillSession("no-such-session", opts)
	if err != nil {
		t.Fatalf("expected nil for nonexistent session, got %v", err)
	}
}

func TestKillSession_KillsSession(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "doomed", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	exists, err := hasSession("doomed", opts)
	if err != nil {
		t.Fatalf("hasSession: %v", err)
	}
	if !exists {
		t.Fatal("session should exist before kill")
	}

	if err := KillSession("doomed", opts); err != nil {
		t.Fatalf("KillSession: %v", err)
	}

	exists, err = hasSession("doomed", opts)
	if err != nil {
		t.Fatalf("hasSession after kill: %v", err)
	}
	if exists {
		t.Fatal("session should not exist after kill")
	}
}

// ---------------------------------------------------------------------------
// AmuxSessionsByWorkspace tests
// ---------------------------------------------------------------------------

func TestAmuxSessionsByWorkspace_Empty(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	m, err := AmuxSessionsByWorkspace(opts)
	if err != nil {
		t.Fatalf("AmuxSessionsByWorkspace: %v", err)
	}
	if len(m) != 0 {
		t.Fatalf("expected empty map, got %v", m)
	}
}

func TestAmuxSessionsByWorkspace_GroupsByWorkspace(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "s1", "sleep 300")
	createSession(t, opts, "s2", "sleep 300")
	createSession(t, opts, "s3", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	setTag(t, opts, "s1", "@amux", "1")
	setTag(t, opts, "s1", "@amux_workspace", "ws-a")
	setTag(t, opts, "s2", "@amux", "1")
	setTag(t, opts, "s2", "@amux_workspace", "ws-a")
	setTag(t, opts, "s3", "@amux", "1")
	setTag(t, opts, "s3", "@amux_workspace", "ws-b")

	m, err := AmuxSessionsByWorkspace(opts)
	if err != nil {
		t.Fatalf("AmuxSessionsByWorkspace: %v", err)
	}
	if len(m["ws-a"]) != 2 {
		t.Fatalf("ws-a: expected 2 sessions, got %v", m["ws-a"])
	}
	if len(m["ws-b"]) != 1 {
		t.Fatalf("ws-b: expected 1 session, got %v", m["ws-b"])
	}
}

func TestAmuxSessionsByWorkspace_IgnoresNonAmux(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "plain", "sleep 300")
	createSession(t, opts, "tagged", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	setTag(t, opts, "tagged", "@amux", "1")
	setTag(t, opts, "tagged", "@amux_workspace", "ws-x")

	m, err := AmuxSessionsByWorkspace(opts)
	if err != nil {
		t.Fatalf("AmuxSessionsByWorkspace: %v", err)
	}
	if len(m) != 1 {
		t.Fatalf("expected 1 workspace, got %v", m)
	}
	if len(m["ws-x"]) != 1 {
		t.Fatalf("ws-x: expected 1 session, got %v", m["ws-x"])
	}
}

func TestAmuxSessionsByWorkspace_SkipsNoWorkspace(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "no-ws", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	setTag(t, opts, "no-ws", "@amux", "1")

	m, err := AmuxSessionsByWorkspace(opts)
	if err != nil {
		t.Fatalf("AmuxSessionsByWorkspace: %v", err)
	}
	if len(m) != 0 {
		t.Fatalf("expected empty map (no workspace tag), got %v", m)
	}
}
