package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The real-binary end of the OSC 7501 Program Status Protocol path: the
// binary's helper dispatch (before TUI/TTY gates), the `amux report` CLI
// emitting a spec-exact sequence through the pane stream, and `amux wait`
// consuming the folded pane options — all against a real isolated tmux
// server, with the tap installed from the real binary like production.

func TestStatusProtocolRealBinary(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	bin, cleanupBin, err := buildAmuxBinary()
	if err != nil {
		t.Fatalf("build amux binary: %v", err)
	}
	defer cleanupBin()

	server := fmt.Sprintf("amux-e2e-status-%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", server, "kill-server").Run() })

	// A pane program that reports through the CLI channel: `amux report`
	// prints one OSC 7501 sequence; the tap installed from the same binary
	// captures it exactly as it would capture any spec-conformant program.
	reporter := t.TempDir() + "/report.sh"
	script := fmt.Sprintf("#!/bin/sh\nsleep 1\n%s report --state blocked --kind question --app e2e-report --message 'needs review'\nsleep 30\n", bin)
	if err := os.WriteFile(reporter, []byte(script), 0o755); err != nil {
		t.Fatalf("write reporter: %v", err)
	}

	// Raw session + manual tap install: this test exercises the real
	// binary's helper process, not the test binary's TestMain dispatch (the
	// in-package tmux tests cover the auto-install path end to end).
	create := exec.Command("tmux", "-L", server, "-f", "/dev/null",
		"new-session", "-d", "-s", "e2e-status", "-x", "200", "-y", "50", reporter)
	if out, err := create.CombinedOutput(); err != nil {
		t.Fatalf("new-session: %v\n%s", err, out)
	}
	// The manual install mirrors tmux.StatusTapCommand's shape exactly:
	// pipe-pane format-expands its command at install time, so '#{pane_id}'
	// bakes in the pane the pipe reads and the helper writes to that pane
	// (a session-targeted write would skew onto the active pane after a
	// split).
	tap := fmt.Sprintf("tmux -L %q -f /dev/null pipe-pane -o -O -t e2e-status %q",
		server, fmt.Sprintf("%q %q %q %q '#{pane_id}'", bin, "--internal-status-tap", server, "e2e-status"))
	if out, err := exec.Command("sh", "-c", tap).CombinedOutput(); err != nil {
		t.Fatalf("tap install: %v\n%s", err, out)
	}

	// The pane's report must surface through the wait CLI. wait derives
	// its server from $TMUX's socket field, exactly as it does inside a
	// real pane; point it at this test server's socket path.
	socket := tmuxSocketPath(server)
	wait := exec.Command(bin, "wait", "--state", "blocked", "--session", "e2e-status", "--timeout", "10s")
	wait.Env = append(os.Environ(), "TMUX="+socket+",0,0")
	out, err := wait.CombinedOutput()
	if err != nil {
		t.Fatalf("amux wait: %v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{"session=e2e-status", "state=blocked", "kind=question", "app=e2e-report", "message=needs review"} {
		if !strings.Contains(got, want) {
			t.Fatalf("wait output missing %q:\n%s", want, got)
		}
	}

	// The helper argv must not leak into normal startup: a malformed tap
	// invocation is a handled usage failure, and an unrelated argv is left
	// to normal dispatch (which, headless, exits on the TTY gate instead
	// of hanging).
	stray := exec.Command(bin, "--internal-status-tap", server)
	stray.Stdin = strings.NewReader("\x1b]7501;state=idle\x07")
	if out, err := stray.CombinedOutput(); err == nil {
		t.Fatalf("malformed tap argv must fail, got success:\n%s", out)
	}
}

// tmuxSocketPath returns the filesystem socket for a -L server name on this
// host, mirroring tmux's own resolution ($TMUX_TMPDIR or /tmp, tmux-UID).
func tmuxSocketPath(server string) string {
	dir := os.Getenv("TMUX_TMPDIR")
	if dir == "" {
		dir = "/tmp"
	}
	return filepath.Join(dir, fmt.Sprintf("tmux-%d", os.Getuid()), server)
}
