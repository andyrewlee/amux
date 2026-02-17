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

	svc, err := NewServices(version)
	if err != nil {
		if gf.JSON {
			ReturnError(w, "init_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "failed to initialize: %v", err)
		}
		return ExitInternalError
	}

	sessionName, found, err := resolveTerminalSessionForWorkspace(wsID, svc.TmuxOpts, svc.QuerySessionRows)
	if err != nil {
		if gf.JSON {
			ReturnError(w, "session_lookup_failed", err.Error(), map[string]any{"workspace_id": string(wsID)}, version)
		} else {
			Errorf(wErr, "failed to lookup terminal session for %s: %v", wsID, err)
		}
		return ExitInternalError
	}
	if !found {
		if gf.JSON {
			ReturnError(w, "not_found", "no terminal session found for workspace", map[string]any{"workspace_id": string(wsID)}, version)
		} else {
			Errorf(wErr, "no terminal session found for workspace %s", wsID)
		}
		return ExitNotFound
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
		if gf.JSON {
			ReturnError(w, "capture_failed", "could not capture pane output", map[string]any{"session_name": sessionName}, version)
		} else {
			Errorf(wErr, "could not capture pane output for session %s", sessionName)
		}
		return ExitNotFound
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
