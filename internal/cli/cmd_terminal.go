package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

type terminalInfo struct {
	SessionName string `json:"session_name"`
	WorkspaceID string `json:"workspace_id"`
	Attached    bool   `json:"attached"`
	CreatedAt   int64  `json:"created_at"`
	AgeSeconds  int64  `json:"age_seconds"`
}

type terminalRunResult struct {
	SessionName string `json:"session_name"`
	WorkspaceID string `json:"workspace_id"`
	Created     bool   `json:"created"`
	Command     string `json:"command"`
}

type terminalLogsResult struct {
	SessionName string `json:"session_name"`
	WorkspaceID string `json:"workspace_id"`
	Lines       int    `json:"lines"`
	Content     string `json:"content"`
}

func routeTerminal(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	if len(args) == 0 {
		if gf.JSON {
			ReturnError(w, "usage_error", "Usage: amux terminal <list|run|logs> [flags]", nil, version)
		} else {
			fmt.Fprintln(wErr, "Usage: amux terminal <list|run|logs> [flags]")
		}
		return ExitUsage
	}
	sub := args[0]
	subArgs := args[1:]
	switch sub {
	case "list", "ls":
		return cmdTerminalList(w, wErr, gf, subArgs, version)
	case "run":
		return cmdTerminalRun(w, wErr, gf, subArgs, version)
	case "logs":
		return cmdTerminalLogs(w, wErr, gf, subArgs, version)
	default:
		if gf.JSON {
			ReturnError(w, "unknown_command", "Unknown terminal subcommand: "+sub, nil, version)
		} else {
			fmt.Fprintf(wErr, "Unknown terminal subcommand: %s\n", sub)
		}
		return ExitUsage
	}
}

func cmdTerminalList(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux terminal list [--workspace <id>] [--json]"
	fs := newFlagSet("terminal list")
	workspace := fs.String("workspace", "", "filter by workspace ID")
	if err := fs.Parse(args); err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}

	filterWS := ""
	if strings.TrimSpace(*workspace) != "" {
		wsID, err := parseWorkspaceIDFlag(*workspace)
		if err != nil {
			return returnUsageError(w, wErr, gf, usage, version, err)
		}
		filterWS = string(wsID)
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

	rows, err := sessionQueryRows(svc.TmuxOpts)
	if err != nil {
		if gf.JSON {
			ReturnError(w, "list_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "failed to list terminal sessions: %v", err)
		}
		return ExitInternalError
	}

	now := time.Now()
	var terminals []terminalInfo
	for _, row := range rows {
		sessionType := strings.TrimSpace(row.tags["@amux_type"])
		if sessionType == "" {
			sessionType = inferSessionType(row.name)
		}
		if !isTermTabType(sessionType) {
			continue
		}
		wsID := strings.TrimSpace(row.tags["@amux_workspace"])
		if wsID == "" {
			wsID = inferWorkspaceID(row.name)
		}
		if filterWS != "" && wsID != filterWS {
			continue
		}
		ageSeconds := int64(0)
		if row.createdAt > 0 {
			ageSeconds = int64(now.Sub(time.Unix(row.createdAt, 0)).Seconds())
			if ageSeconds < 0 {
				ageSeconds = 0
			}
		}
		terminals = append(terminals, terminalInfo{
			SessionName: row.name,
			WorkspaceID: wsID,
			Attached:    row.attached,
			CreatedAt:   row.createdAt,
			AgeSeconds:  ageSeconds,
		})
	}

	if gf.JSON {
		PrintJSON(w, terminals, version)
		return ExitOK
	}

	PrintHuman(w, func(w io.Writer) {
		if len(terminals) == 0 {
			fmt.Fprintln(w, "No terminal sessions.")
			return
		}
		for _, t := range terminals {
			attached := ""
			if t.Attached {
				attached = " (attached)"
			}
			fmt.Fprintf(w, "  %-45s ws=%-16s age=%s%s\n",
				t.SessionName, t.WorkspaceID, formatAge(t.AgeSeconds), attached)
		}
	})
	return ExitOK
}

func cmdTerminalRun(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux terminal run --workspace <id> --text <command> [--enter=true] [--create=true] [--json]"
	fs := newFlagSet("terminal run")
	workspace := fs.String("workspace", "", "workspace ID (required)")
	text := fs.String("text", "", "command text to send (required)")
	enter := fs.Bool("enter", true, "send Enter key after text")
	create := fs.Bool("create", true, "create terminal session when missing")
	if err := fs.Parse(args); err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if fs.NArg() > 0 {
		return returnUsageError(
			w,
			wErr,
			gf,
			usage,
			version,
			fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " ")),
		)
	}
	if strings.TrimSpace(*workspace) == "" || strings.TrimSpace(*text) == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
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

	sessionName, found, err := resolveTerminalSessionForWorkspace(wsID, svc.TmuxOpts)
	if err != nil {
		if gf.JSON {
			ReturnError(w, "session_lookup_failed", err.Error(), map[string]any{"workspace_id": string(wsID)}, version)
		} else {
			Errorf(wErr, "failed to lookup terminal session for %s: %v", wsID, err)
		}
		return ExitInternalError
	}

	created := false
	if !found {
		if !*create {
			if gf.JSON {
				ReturnError(w, "not_found", "no terminal session found for workspace", map[string]any{"workspace_id": string(wsID)}, version)
			} else {
				Errorf(wErr, "no terminal session found for workspace %s", wsID)
			}
			return ExitNotFound
		}
		ws, err := svc.Store.Load(wsID)
		if err != nil {
			if gf.JSON {
				ReturnError(w, "not_found", fmt.Sprintf("workspace %s not found", wsID), nil, version)
			} else {
				Errorf(wErr, "workspace %s not found", wsID)
			}
			return ExitNotFound
		}
		sessionName, err = createWorkspaceTerminalSession(ws, wsID, svc.TmuxOpts)
		if err != nil {
			if gf.JSON {
				ReturnError(w, "session_create_failed", err.Error(), map[string]any{"workspace_id": string(wsID)}, version)
			} else {
				Errorf(wErr, "failed to create terminal session for %s: %v", wsID, err)
			}
			return ExitInternalError
		}
		created = true
	}

	command := *text
	if err := tmuxSendKeys(sessionName, command, *enter, svc.TmuxOpts); err != nil {
		if gf.JSON {
			ReturnError(w, "send_failed", err.Error(), map[string]any{
				"workspace_id": string(wsID),
				"session_name": sessionName,
			}, version)
		} else {
			Errorf(wErr, "failed to send command to %s: %v", sessionName, err)
		}
		return ExitInternalError
	}

	result := terminalRunResult{
		SessionName: sessionName,
		WorkspaceID: string(wsID),
		Created:     created,
		Command:     command,
	}
	if gf.JSON {
		PrintJSON(w, result, version)
		return ExitOK
	}
	PrintHuman(w, func(w io.Writer) {
		createdSuffix := ""
		if created {
			createdSuffix = " (created)"
		}
		fmt.Fprintf(w, "Sent to terminal %s%s\n", sessionName, createdSuffix)
	})
	return ExitOK
}

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

	sessionName, found, err := resolveTerminalSessionForWorkspace(wsID, svc.TmuxOpts)
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

func resolveTerminalSessionForWorkspace(wsID data.WorkspaceID, opts tmux.Options) (string, bool, error) {
	rows, err := sessionQueryRows(opts)
	if err != nil {
		return "", false, err
	}
	target := string(wsID)

	bestName := ""
	bestAttached := false
	bestCreated := int64(-1)
	for _, row := range rows {
		sessionType := strings.TrimSpace(row.tags["@amux_type"])
		if sessionType == "" {
			sessionType = inferSessionType(row.name)
		}
		if !isTermTabType(sessionType) {
			continue
		}
		rowWSID := strings.TrimSpace(row.tags["@amux_workspace"])
		if rowWSID == "" {
			rowWSID = inferWorkspaceID(row.name)
		}
		if rowWSID != target {
			continue
		}
		if bestName == "" ||
			(row.attached && !bestAttached) ||
			(row.attached == bestAttached && row.createdAt > bestCreated) {
			bestName = row.name
			bestAttached = row.attached
			bestCreated = row.createdAt
		}
	}
	if bestName == "" {
		return "", false, nil
	}
	return bestName, true, nil
}

func createWorkspaceTerminalSession(ws *data.Workspace, wsID data.WorkspaceID, opts tmux.Options) (string, error) {
	if ws == nil {
		return "", errors.New("workspace is required")
	}
	root := strings.TrimSpace(ws.Root)
	if root == "" {
		return "", errors.New("workspace root is empty")
	}

	tabID := "term-tab-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	sessionName := tmux.SessionName("amux", string(wsID), tabID)
	createArgs := []string{
		"new-session", "-d", "-s", sessionName, "-c", root, terminalShellCommand(),
	}
	cmd, cancel := tmuxStartSession(opts, createArgs...)
	defer cancel()
	if err := cmd.Run(); err != nil {
		return "", err
	}

	now := strconv.FormatInt(time.Now().Unix(), 10)
	tags := []struct {
		Key   string
		Value string
	}{
		{Key: "@amux", Value: "1"},
		{Key: "@amux_workspace", Value: string(wsID)},
		{Key: "@amux_tab", Value: tabID},
		{Key: "@amux_type", Value: "terminal"},
		{Key: "@amux_assistant", Value: "terminal"},
		{Key: "@amux_created_at", Value: now},
	}
	for _, tag := range tags {
		if err := tmuxSetSessionTag(sessionName, tag.Key, tag.Value, opts); err != nil {
			_ = tmuxKillSession(sessionName, opts)
			return "", fmt.Errorf("failed to set %s: %w", tag.Key, err)
		}
	}
	return sessionName, nil
}

func terminalShellCommand() string {
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		return "sh"
	}
	return shell
}
