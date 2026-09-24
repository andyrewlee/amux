package tmux

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/andyrewlee/amux/internal/shellutil"
)

// EnsureDetachedSession creates sessionName running command in workDir with no
// attached client — the hosting shape for workspace `run` scripts, which need
// scrollback and crash forensics rather than an interactive tab.
//
// The session gets the same managed settings and @amux_* tags as attached
// sessions plus `remain-on-exit on`: when the command exits the pane stays
// (pane_dead=1, pane_dead_status=<exit code>) so its output remains
// capturable until the session is explicitly killed. Callers distinguish
// "alive" from "exited" via RunSessionStatus, not has-session alone.
func EnsureDetachedSession(sessionName, workDir, command string, environment []string, opts Options, tags SessionTags) error {
	if opts == (Options{}) {
		opts = DefaultOptions()
	}
	if opts.ConfigPath != "" && !filepath.IsAbs(opts.ConfigPath) {
		// Mirror clientCommand: relative config paths resolve against the
		// workspace so the spawned tmux finds the file the caller meant.
		opts.ConfigPath = filepath.Join(workDir, opts.ConfigPath)
	}
	base := tmuxBase(opts)
	dir := shellutil.ShellQuote(workDir)
	optionTgt := shellutil.ShellQuote(exactSessionOptionTarget(sessionName))

	paneCommand := paneLaunchCommand(command, dir, environment)
	settings := append(sessionSettingArgs(optionTgt, opts, tags),
		[]string{"-t", optionTgt, "remain-on-exit", "on"})

	// ensure-then-settings; the braced settings fallback cannot re-run
	// ensureSession on its own failure (settingsScript already self-heals
	// per-option), matching clientCommand's bracing discipline. The script
	// text already ends with "; " and always exits true.
	script := fmt.Sprintf("%s && { %s}", ensureSessionScript(base, sessionName, dir, paneCommand), settingsScript(base, settings))

	ctx, cancel := context.WithTimeout(context.Background(), tmuxCommandTimeout)
	defer cancel()
	// #nosec G204 -- the script is built from shell-quoted parts only.
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	if out, err := runTmuxCmdCombined(cmd); err != nil {
		return fmt.Errorf("ensure detached session %s: %w (%s)", sessionName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RunSessionStatus reports a detached run session's state: whether it exists,
// whether its pane command is still running (pane_dead=0), and the exit code
// recorded by remain-on-exit when it finished (pane_dead_status; -1 when
// unavailable). A missing session returns exists=false.
func RunSessionStatus(sessionName string, opts Options) (exists, alive bool, exitCode int, err error) {
	if sessionName == "" {
		return false, false, -1, nil
	}
	if err := EnsureAvailable(); err != nil {
		return false, false, -1, err
	}
	// display-message -t does not support the "=" exact-match form (like
	// set-option/show-options). Two failure modes shape the check below:
	// a missing session exits 0 with EMPTY output, and a bare-name target can
	// prefix-match another session — so the format echoes #{session_name} and
	// only an exact echo counts as existing.
	cmd, cancel := tmuxCommand(opts, "display-message", "-p", "-t", sessionName,
		"#{session_name}\t#{pane_dead}\t#{pane_dead_status}")
	output, err := runTmuxCmdCombined(cmd)
	cancel()
	if err != nil {
		if isSessionNotFoundStderr(string(output)) || isExitCode1(err) {
			return false, false, -1, nil
		}
		return false, false, -1, err
	}
	// Trim only line terminators, not all whitespace: a live pane reports an
	// EMPTY pane_dead_status ("name\t0\t\n"), and a full TrimSpace would eat
	// the interior tab too — collapsing to two fields and falsely reporting
	// the session missing (live run sessions must return exists=true).
	fields := strings.Split(strings.TrimRight(string(output), "\r\n"), "\t")
	if len(fields) < 3 || fields[0] != sessionName {
		return false, false, -1, nil
	}
	dead := strings.TrimSpace(fields[1]) == "1"
	exitCode = -1
	if dead {
		if code, cerr := strconv.Atoi(strings.TrimSpace(fields[2])); cerr == nil {
			exitCode = code
		}
	}
	return true, !dead, exitCode, nil
}

// RunSessionTail captures up to lines of the named session's pane content,
// including scrollback, and works on dead (remain-on-exit) panes — the whole
// point of the run-session shape. Returns ("", false) when the session or a
// capturable pane does not exist.
//
// It cannot reuse CapturePaneTail: that path's pane resolution deliberately
// skips dead panes (an agent tab wants a live pane or nothing), while a run
// session's forensics live exactly in the dead pane.
func RunSessionTail(sessionName string, lines int, opts Options) (string, bool) {
	if sessionName == "" || lines <= 0 {
		return "", false
	}
	if err := EnsureAvailable(); err != nil {
		return "", false
	}
	// Resolve a pane ID ourselves rather than display-message: display-message
	// -t does not support the "=" target form, and a dead active pane is a
	// valid capture target here (unlike sessionPaneID's live-only pick).
	rows, err := listTmux(opts, "list-panes", "-t", sessionTarget(sessionName),
		"-F", "#{pane_id}\t#{pane_active}")
	if err != nil || len(rows) == 0 {
		return "", false
	}
	paneID := ""
	for _, row := range rows {
		parts := strings.Split(row, "\t")
		if len(parts) < 2 {
			continue
		}
		id := strings.TrimSpace(parts[0])
		if id == "" || id[0] != '%' {
			continue
		}
		if paneID == "" || strings.TrimSpace(parts[1]) == "1" {
			paneID = id
			if strings.TrimSpace(parts[1]) == "1" {
				break
			}
		}
	}
	if paneID == "" {
		return "", false
	}
	cmd, cancel := tmuxCommand(opts, "capture-pane", "-p", "-t", paneID, "-S", strconv.Itoa(-lines))
	defer cancel()
	out, err := runTmuxCmd(cmd)
	if err != nil {
		return "", false
	}
	return strings.TrimRight(string(out), " \t\n\r"), true
}

// FindRunSessions returns the names of run sessions a workspace owns —
// @amux_workspace + @amux_type=run. When instanceID is non-empty, rows are
// additionally filtered to sessions whose @amux_instance shares the same
// state namespace (the half before the per-launch random suffix): a restarted
// amux still sees its own run sessions, while a genuinely foreign instance
// (different state root sharing the tmux server) is excluded — the same
// scoping the GC stub tests exercise via instancesShareState.
func FindRunSessions(workspaceID, instanceID string, opts Options) ([]string, error) {
	rows, err := SessionsWithTags(map[string]string{
		"@amux":           "1",
		"@amux_workspace": workspaceID,
		"@amux_type":      "run",
	}, []string{"@amux_instance"}, opts)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		if instanceID != "" && !instanceNamespacesMatch(row.Tags["@amux_instance"], instanceID) {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}

// instanceNamespacesMatch reports whether two instance IDs share the state
// namespace — the "<hash>." prefix before the per-launch random half. Empty
// tags (sessions created before instance tagging) always match.
func instanceNamespacesMatch(taggedInstance, instanceID string) bool {
	if taggedInstance == "" {
		return true
	}
	ns := func(id string) string {
		if i := strings.Index(id, "."); i >= 0 {
			return id[:i]
		}
		return id
	}
	return ns(taggedInstance) == ns(instanceID)
}
