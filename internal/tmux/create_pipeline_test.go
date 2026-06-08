package tmux

import (
	"fmt"
	"os/exec"
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
			ServerName:   opts.ServerName,
			ConfigPath:   opts.ConfigPath,
			HideStatus:   true,
			DisableMouse: true,
		},
		Tags:           SessionTags{WorkspaceID: "ws-create", Type: "agent", Assistant: "claude"},
		DetachExisting: true,
	})

	// The create + set-option chain runs before the final `attach`, which fails
	// without a controlling terminal. Ignore that failure: the settings already
	// applied, which is what we verify.
	_ = exec.Command("sh", "-c", cmdStr).Run()

	waitForSessionExists(t, opts, session)

	checks := []struct{ key, want string }{
		{"prefix", "None"},
		{"status", "off"},
		{"mouse", "off"},
		{"@amux", "1"},
		{"@amux_workspace", "ws-create"},
		{"@amux_type", "agent"},
		{"@amux_assistant", "claude"},
	}
	for _, c := range checks {
		if got := showSessionOption(t, opts, session, c.key); got != c.want {
			t.Errorf("session option %s = %q, want %q (a rejected set-option would no-op here)", c.key, got, c.want)
		}
	}
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
