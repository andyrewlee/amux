package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func cmdTerminalLogs(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux terminal logs --workspace <id> [--lines N] [--follow] [--interval <duration>] [--idle-threshold <duration>] [--json]"
	fs := newFlagSet("terminal logs")
	workspace := fs.String("workspace", "", "workspace ID (required)")
	lines := fs.Int("lines", 200, "number of lines to capture")
	follow := fs.Bool("follow", false, "stream terminal output as NDJSON")
	interval := fs.Duration("interval", 500*time.Millisecond, "poll interval when --follow")
	idleThreshold := fs.Duration("idle-threshold", 5*time.Second, "idle event threshold when --follow")
	if err := fs.Parse(args); err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if strings.TrimSpace(*workspace) == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if *lines <= 0 {
		return returnUsageError(w, wErr, gf, usage, version, errors.New("--lines must be > 0"))
	}
	if *follow {
		if *interval <= 0 {
			return returnUsageError(w, wErr, gf, usage, version, errors.New("--interval must be > 0"))
		}
		if *idleThreshold <= 0 {
			return returnUsageError(w, wErr, gf, usage, version, errors.New("--idle-threshold must be > 0"))
		}
	}

	wsID, err := parseWorkspaceIDFlag(*workspace)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}

	svc, code := initServicesOrFail(w, wErr, gf, version)
	if code >= 0 {
		return code
	}

	sessionName, found, err := resolveTerminalSessionForWorkspace(wsID, svc.TmuxOpts, svc.QuerySessionRows)
	if err != nil {
		return returnOperationError(w, wErr, gf, version,
			ExitInternalError, "session_lookup_failed", err, map[string]any{"workspace_id": string(wsID)},
			"failed to lookup terminal session for %s: %v", wsID, err)
	}
	if !found {
		return returnOperationError(w, wErr, gf, version,
			ExitNotFound, "not_found", fmt.Errorf("no terminal session found for workspace"),
			map[string]any{"workspace_id": string(wsID)},
			"no terminal session found for workspace %s", wsID)
	}

	if *follow {
		cfg := watchConfig{
			SessionName:   sessionName,
			Lines:         *lines,
			Interval:      *interval,
			IdleThreshold: *idleThreshold,
		}
		ctx, cancel := contextWithSignal()
		defer cancel()
		return runWatchLoop(ctx, w, cfg, svc.TmuxOpts)
	}

	content, ok := tmux.CapturePaneTail(sessionName, *lines, svc.TmuxOpts)
	if !ok {
		return returnOperationError(w, wErr, gf, version,
			ExitNotFound, "capture_failed", fmt.Errorf("could not capture pane output"),
			map[string]any{"session_name": sessionName},
			"could not capture pane output for session %s", sessionName)
	}

	result := terminalLogsResult{
		SessionName: sessionName,
		WorkspaceID: string(wsID),
		Lines:       *lines,
		Content:     content,
	}
	if gf.JSON {
		PrintJSON(w, result, version)
		return ExitOK
	}
	PrintHuman(w, func(w io.Writer) {
		fmt.Fprint(w, content)
		if content != "" && content[len(content)-1] != '\n' {
			fmt.Fprintln(w)
		}
	})
	return ExitOK
}
