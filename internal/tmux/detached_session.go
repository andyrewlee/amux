package tmux

import (
	"context"
	"errors"
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

	launch, err := prepareLaunch(workDir, command, environment)
	if err != nil {
		return err
	}
	settings := append(sessionSettingArgs(optionTgt, opts, tags),
		[]string{"-t", optionTgt, "remain-on-exit", "on"})

	// ensure-then-settings; the braced settings fallback cannot re-run
	// ensureSession on its own failure (settingsScript already self-heals
	// per-option), matching clientCommand's bracing discipline. The script
	// text already ends with "; " and always exits true.
	script := fmt.Sprintf("%s && { %s}", ensureSessionScript(base, sessionName, dir, launch, true), settingsScript(base, settings))

	ctx, cancel := context.WithTimeout(context.Background(), tmuxCommandTimeout)
	defer cancel()
	// #nosec G204 -- the script is built from shell-quoted parts only.
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	if out, err := runTmuxCmdCombined(cmd); err != nil {
		if ctx.Err() == nil {
			// A non-timeout failure means every ensure branch failed, so no
			// session exists and no pane can be pending on this payload —
			// the attempt is provably unused. A killed/timed-out script is
			// ambiguous: the create may already have dispatched, so the
			// expiring payload must be left for the consumer or the sweep.
			_ = launch.payload.Discard()
		}
		return fmt.Errorf("ensure detached session %s: %w (%s)", sessionName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ErrSessionNameTaken reports a create-only allocation losing the name: a
// session already owned it when the create ran. Unlike EnsureDetachedSession
// — create-unless-present for reattach callers — CreateDetachedSession never
// adopts or re-tags the existing session; the caller picks a fresh name and
// retries.
var ErrSessionNameTaken = errors.New("tmux session name already taken")

// CreateDetachedSession creates sessionName running command in workDir with
// no attached client, or reports ErrSessionNameTaken when the name is taken.
// It is the allocation path for fresh run sessions: two concurrent starters
// can pick the same candidate name, and adoption would let both report
// success on one session while the loser's tags overwrite the winner's.
//
// Session shape matches EnsureDetachedSession — same managed settings,
// @amux_* tags, and remain-on-exit — but the settings run only when this
// call owns the create, so a foreign session's tags are never touched.
func CreateDetachedSession(sessionName, workDir, command string, environment []string, opts Options, tags SessionTags) error {
	if opts == (Options{}) {
		opts = DefaultOptions()
	}
	if opts.ConfigPath != "" && !filepath.IsAbs(opts.ConfigPath) {
		opts.ConfigPath = filepath.Join(workDir, opts.ConfigPath)
	}
	base := tmuxBase(opts)
	dir := shellutil.ShellQuote(workDir)
	optionTgt := shellutil.ShellQuote(exactSessionOptionTarget(sessionName))

	launch, err := prepareLaunch(workDir, command, environment)
	if err != nil {
		return err
	}
	settings := append(sessionSettingArgs(optionTgt, opts, tags),
		[]string{"-t", optionTgt, "remain-on-exit", "on"})

	script := createSessionScript(base, sessionName, dir, launch, true, settingsScript(base, settings))

	ctx, cancel := context.WithTimeout(context.Background(), tmuxCommandTimeout)
	defer cancel()
	// #nosec G204 -- the script is built from shell-quoted parts only.
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	out, err := runTmuxCmdCombined(cmd)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == exitCodeCollision {
			return fmt.Errorf("create detached session %s: %w", sessionName, ErrSessionNameTaken)
		}
		if ctx.Err() == nil {
			// Same reasoning as EnsureDetachedSession: a non-timeout failure
			// means the create didn't dispatch a pane on this payload.
			_ = launch.payload.Discard()
		}
		return fmt.Errorf("create detached session %s: %w (%s)", sessionName, err, strings.TrimSpace(string(out)))
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
		} else if code, ok := deadPaneBannerStatus(sessionName, opts); ok {
			// pane_dead_status does not exist before tmux 3.3; the
			// remain-on-exit banner still prints "(status N)" on older
			// servers, so fall back to parsing it from the dead pane.
			exitCode = code
		}
	}
	return true, !dead, exitCode, nil
}

// deadPaneBannerStatus reads the exit code from the remain-on-exit banner
// rendered inside the dead pane ("Pane is dead (status 7)") — the fallback
// for tmux versions without the pane_dead_status format (< 3.3). The extra
// capture only runs when a dead pane reported no status, so newer servers
// never pay for it.
func deadPaneBannerStatus(sessionName string, opts Options) (int, bool) {
	out, ok := RunSessionTail(sessionName, 10, opts)
	if !ok {
		return 0, false
	}
	return parseDeadPaneBannerStatus(out)
}

// parseDeadPaneBannerStatus extracts the exit code from the last
// "Pane is dead (status N)" line in captured pane text.
func parseDeadPaneBannerStatus(paneText string) (int, bool) {
	const marker = "Pane is dead (status "
	idx := strings.LastIndex(paneText, marker)
	if idx < 0 {
		return 0, false
	}
	rest := paneText[idx+len(marker):]
	end := strings.IndexByte(rest, ')')
	if end <= 0 {
		return 0, false
	}
	code, err := strconv.Atoi(strings.TrimSpace(rest[:end]))
	if err != nil {
		return 0, false
	}
	return code, true
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
	rows, err := FindRunSessionsDetailed(workspaceID, instanceID, nil, opts)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return names, nil
}

// FindRunSessionsDetailed is FindRunSessions with tag values: the same
// @amux_workspace+type=run ownership filter and instance scoping, but each
// row carries the requested extra @amux_* keys (@amux_instance is always
// fetched for the filter). Newest-session selection uses this to order on
// @amux_created_at instead of name suffixes.
func FindRunSessionsDetailed(workspaceID, instanceID string, keys []string, opts Options) ([]SessionTagValues, error) {
	rows, err := SessionsWithTags(map[string]string{
		"@amux":           "1",
		"@amux_workspace": workspaceID,
		"@amux_type":      "run",
	}, append(append([]string{}, keys...), "@amux_instance"), opts)
	if err != nil {
		return nil, err
	}
	out := make([]SessionTagValues, 0, len(rows))
	for _, row := range rows {
		row.Name = strings.TrimSpace(row.Name)
		if row.Name == "" {
			continue
		}
		if instanceID != "" && !instanceNamespacesMatch(row.Tags["@amux_instance"], instanceID) {
			continue
		}
		out = append(out, row)
	}
	return out, nil
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
