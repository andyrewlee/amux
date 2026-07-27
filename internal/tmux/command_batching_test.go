package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The session settings are chained into a single tmux invocation because amux
// talks to one shared, single-threaded tmux server: on a busy server each exec
// costs tens to hundreds of milliseconds, and a reattach used to pay for one per
// option. tmux aborts a chained sequence at the first failure, so the script
// falls back to running them one at a time. These tests pin both branches with a
// fake tmux, since a silently-skipped option is exactly the failure mode the
// `2>/dev/null` suppression would otherwise hide.

// fakeTmux puts a recording `tmux` on PATH. It appends one line per invocation
// to a log file and exits 1 for any invocation mentioning failOn (empty means
// never fail), mimicking tmux's "invalid option" rejection.
func fakeTmux(t *testing.T, failOn string) string {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
case "$*" in
  *%s*) echo "invalid option" >&2; exit 1 ;;
esac
exit 0
`, logPath, failOn)
	if failOn == "" {
		// An unmatchable pattern keeps the case arm valid while never failing.
		script = strings.Replace(script, "**)", "*__never_matches__*)", 1)
	}
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func runSettingsScript(t *testing.T, logPath, script string) []string {
	t.Helper()
	// The script's own exit status is not meaningful: it ends with "; " so the
	// attach that normally follows runs regardless.
	_ = exec.Command("sh", "-c", script).Run()

	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil
	}
	var calls []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			calls = append(calls, line)
		}
	}
	return calls
}

func testSettings() [][]string {
	return sessionSettingArgs("'sess'", Options{
		HideStatus:      true,
		DisableMouse:    true,
		DefaultTerminal: "xterm-256color",
	}, SessionTags{WorkspaceID: "w", TabID: "t", Type: "agent"})
}

// wantAppliedOptions is every option name testSettings produces, so a test can
// assert none of them was skipped.
func wantAppliedOptions() []string {
	return []string{
		"prefix", "prefix2", "status", "mouse", "default-terminal",
		"monitor-activity", "@amux", "@amux_workspace", "@amux_tab", "@amux_type",
	}
}

func assertAllOptionsApplied(t *testing.T, calls []string) {
	t.Helper()
	joined := strings.Join(calls, "\n")
	for _, opt := range wantAppliedOptions() {
		if !strings.Contains(joined, opt) {
			t.Errorf("option %q was never applied; calls:\n%s", opt, joined)
		}
	}
}

func TestSettingsScript_ChainsIntoOneInvocationOnSuccess(t *testing.T) {
	logPath := fakeTmux(t, "")
	calls := runSettingsScript(t, logPath, settingsScript("tmux", testSettings()))

	if len(calls) != 1 {
		t.Fatalf("expected every set-option chained into one tmux invocation, got %d:\n%s",
			len(calls), strings.Join(calls, "\n"))
	}
	assertAllOptionsApplied(t, calls)
}

func TestSettingsScript_FallsBackToOneInvocationPerOption(t *testing.T) {
	// tmux stops a chained sequence at the first rejected command, so a single
	// unsupported option would otherwise skip every option after it.
	logPath := fakeTmux(t, "default-terminal")
	calls := runSettingsScript(t, logPath, settingsScript("tmux", testSettings()))

	if len(calls) < 2 {
		t.Fatalf("expected a per-option fallback after the chain failed, got %d invocation(s):\n%s",
			len(calls), strings.Join(calls, "\n"))
	}
	// Everything except the genuinely unsupported option must still be applied,
	// including the ones that would have followed it in the chain.
	for _, opt := range wantAppliedOptions() {
		if opt == "default-terminal" {
			continue
		}
		var applied bool
		for _, call := range calls[1:] {
			if strings.Contains(call, opt) {
				applied = true
				break
			}
		}
		if !applied {
			t.Errorf("option %q was skipped by the fallback; calls:\n%s", opt, strings.Join(calls, "\n"))
		}
	}
}

func TestSettingsScript_EmptySettingsProducesNoScript(t *testing.T) {
	if got := settingsScript("tmux", nil); got != "" {
		t.Fatalf("no settings must produce no script, got %q", got)
	}
}

func TestSettingsScript_AlwaysEndsSoAttachStillRuns(t *testing.T) {
	// The attach is appended directly after this script, so it must terminate
	// with an unconditional separator rather than && — a failed set-option must
	// never prevent the user's session from being attached.
	script := settingsScript("tmux", testSettings())
	if !strings.HasSuffix(script, "; ") {
		t.Fatalf("settings script must end with an unconditional separator, got %q", script)
	}

	logPath := fakeTmux(t, "set-option")
	// Every set-option fails, chained and sequential alike; the marker still runs.
	marker := filepath.Join(t.TempDir(), "attached")
	_ = exec.Command("sh", "-c", script+"touch "+marker).Run()
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected the attach to run even when every set-option failed: %v", err)
	}
	if calls := runSettingsScript(t, logPath, script); len(calls) == 0 {
		t.Fatal("expected the fake tmux to have been invoked")
	}
}

func TestSessionSettingArgs_OmitsDisabledOptions(t *testing.T) {
	args := sessionSettingArgs("'sess'", Options{}, SessionTags{})
	joined := fmt.Sprint(args)
	for _, unwanted := range []string{"status", "mouse", "default-terminal", "@amux"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("expected %q to be omitted when unconfigured, got %v", unwanted, args)
		}
	}
	// The prefix disabling and activity monitoring are unconditional: they are
	// what make a managed session transparent and trackable.
	for _, required := range []string{"prefix", "prefix2", "monitor-activity"} {
		if !strings.Contains(joined, required) {
			t.Errorf("expected %q to always be applied, got %v", required, args)
		}
	}
}
