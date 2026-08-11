package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// realTmuxServerWithKeepalive returns Options for an isolated tmux server kept
// alive by a detached session. The shared ensureTmuxServer uses a bare
// `start-server`, which self-exits with no sessions and so skips even where tmux
// works; a keepalive session avoids that so this test actually runs.
func realTmuxServerWithKeepalive(t *testing.T) Options {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	name := fmt.Sprintf("amux-create-pipeline-%d", time.Now().UnixNano())
	opts := Options{ServerName: name, ConfigPath: "/dev/null", CommandTimeout: 5 * time.Second}
	keep := exec.Command("tmux", tmuxArgs(opts, "new-session", "-d", "-s", "_keepalive", "sleep", "300")...)
	if out, err := keep.CombinedOutput(); err != nil {
		t.Skipf("tmux unusable: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", name, "kill-server").Run()
	})
	return opts
}

// TestClientCommandAppliesOptionsAndTagsOnRealTmux executes the real
// session-create pipeline (NewClientCommand) against a live tmux server and
// reads back the options and tags it set. The pipeline suppresses every
// set-option error with `2>/dev/null`, so a rejected target syntax — exactly
// bug #1, where tmux 3.6a rejects an '='-prefixed set-option target — would
// silently no-op and ship undetected. Asserting the values actually applied is
// the only thing that catches that; the existing tests only string-match the
// generated command.
func TestClientCommandAppliesOptionsAndTagsOnRealTmux(t *testing.T) {
	opts := realTmuxServerWithKeepalive(t)
	const session = "create-pipeline"

	cmdStr := NewClientCommand(session, ClientCommandParams{
		WorkDir: t.TempDir(),
		Command: "sleep 300",
		Options: Options{
			ServerName:      opts.ServerName,
			ConfigPath:      opts.ConfigPath,
			HideStatus:      true,
			DisableMouse:    true,
			DefaultTerminal: "xterm-256color",
		},
		Tags: SessionTags{
			WorkspaceID:  "ws-create",
			TabID:        "tab-create",
			Type:         "agent",
			Assistant:    "claude",
			CreatedAt:    1700000000,
			InstanceID:   "inst-create",
			SessionOwner: "inst-create",
			LeaseAtMS:    1700000000000,
		},
		DetachExisting: true,
	})

	// The create + set-option chain runs before the final `attach`, which fails
	// without a controlling terminal. Ignore that failure: the settings already
	// applied, which is what we verify.
	_ = exec.Command("sh", "-c", cmdStr).Run()

	waitForSessionExists(t, opts, session)

	// Every option the pipeline sets, not a sample: these are now applied as one
	// chained tmux invocation, so a single rejected command would silently skip
	// every option after it. Checking only a few would leave that gap open.
	checks := []struct{ key, want string }{
		{"prefix", "None"},
		{"prefix2", "None"},
		{"status", "off"},
		{"mouse", "off"},
		{"default-terminal", "xterm-256color"},
		{"@amux", "1"},
		{"@amux_workspace", "ws-create"},
		{"@amux_tab", "tab-create"},
		{"@amux_type", "agent"},
		{"@amux_assistant", "claude"},
		{"@amux_created_at", "1700000000"},
		{"@amux_instance", "inst-create"},
		{TagSessionOwner, "inst-create"},
		{TagSessionLeaseAt, "1700000000000"},
		{TagSessionOwnerHeartbeatAt, "1700000000000"},
	}
	for _, c := range checks {
		if got := showSessionOption(t, opts, session, c.key); got != c.want {
			t.Errorf("session option %s = %q, want %q (a rejected set-option would no-op here)", c.key, got, c.want)
		}
	}

	// monitor-activity is the only chained entry carrying a scope flag (-w), and
	// the pre-attach quiet check depends on it. A mid-chain -w must scope to its
	// own command and not leak into the entries that follow, which the tag checks
	// above would catch.
	if got := showWindowOption(t, opts, session, "monitor-activity"); got != "on" {
		t.Errorf("window option monitor-activity = %q, want \"on\" — the activity-based quiet check depends on it", got)
	}
}

// TestClientCommandEscapesDeletedServerWorkingDirectory reproduces the tmux
// failure that poisoned every workspace created after deleting the workspace
// from which the shared server originally started. tmux accepts -c and records
// it as session_path, but 3.7b still leaves the pane process in the deleted
// server cwd. The explicit-cd trampoline must put the process in the new
// workspace before the final shell (or agent) starts.
func TestClientCommandEscapesDeletedServerWorkingDirectory(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	serverDir := t.TempDir()
	workspaceDir := t.TempDir()
	serverName := fmt.Sprintf("amux-deleted-cwd-%d", time.Now().UnixNano())
	opts := Options{ServerName: serverName, ConfigPath: "/dev/null", CommandTimeout: 5 * time.Second}

	keep := exec.Command("tmux", tmuxArgs(opts, "new-session", "-d", "-s", "_keepalive", "sleep", "300")...)
	keep.Dir = serverDir
	if out, err := keep.CombinedOutput(); err != nil {
		t.Skipf("tmux unusable: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", serverName, "kill-server").Run()
	})

	if err := os.Remove(serverDir); err != nil {
		t.Fatalf("remove tmux server cwd: %v", err)
	}

	const session = "deleted-cwd-recovery"
	cmdStr := NewClientCommand(session, ClientCommandParams{
		WorkDir:     workspaceDir,
		Command:     `test "$WORKSPACE_SENTINEL" = recovered && sleep 300`,
		Environment: []string{"WORKSPACE_SENTINEL=recovered"},
		Options:     opts,
	})
	// The final attach requires a controlling terminal. Session creation runs
	// before it, so the attach error is intentionally ignored here.
	_ = exec.Command("sh", "-c", cmdStr).Run()
	waitForSessionExists(t, opts, session)

	out, err := exec.Command("tmux", tmuxArgs(opts, "list-panes", "-t", session, "-F", "#{pane_current_path}")...).CombinedOutput()
	if err != nil {
		t.Fatalf("read recovered pane cwd: %v\n%s", err, out)
	}
	got, err := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("resolve recovered pane cwd %q: %v", strings.TrimSpace(string(out)), err)
	}
	want, err := filepath.EvalSymlinks(workspaceDir)
	if err != nil {
		t.Fatalf("resolve workspace cwd %q: %v", workspaceDir, err)
	}
	if got != want {
		t.Fatalf("pane cwd = %q, want workspace cwd %q", got, want)
	}
}

func showWindowOption(t *testing.T, opts Options, session, key string) string {
	t.Helper()
	out, err := exec.Command("tmux", tmuxArgs(opts, "show-options", "-w", "-t", session, "-v", key)...).CombinedOutput()
	if err != nil {
		t.Fatalf("show-options -w %s on %s: %v\n%s", key, session, err, out)
	}
	return strings.TrimSpace(string(out))
}

func waitForSessionExists(t *testing.T, opts Options, session string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		out, _ := exec.Command("tmux", tmuxArgs(opts, "list-sessions", "-F", "#{session_name}")...).CombinedOutput()
		for _, line := range strings.Split(string(out), "\n") {
			if strings.TrimSpace(line) == session {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("session %q never appeared on the server", session)
}

func showSessionOption(t *testing.T, opts Options, session, key string) string {
	t.Helper()
	out, err := exec.Command("tmux", tmuxArgs(opts, "show-options", "-t", session, "-v", key)...).CombinedOutput()
	if err != nil {
		t.Fatalf("show-options %s on %s: %v\n%s", key, session, err, out)
	}
	return strings.TrimSpace(string(out))
}
