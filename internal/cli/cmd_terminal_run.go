package cli

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

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

	sessionName, found, err := resolveTerminalSessionForWorkspace(wsID, svc.TmuxOpts, svc.QuerySessionRows)
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

func resolveTerminalSessionForWorkspace(wsID data.WorkspaceID, opts tmux.Options, queryRows ...func(tmux.Options) ([]sessionRow, error)) (string, bool, error) {
	queryFn := defaultQuerySessionRows
	if len(queryRows) > 0 && queryRows[0] != nil {
		queryFn = queryRows[0]
	}
	rows, err := queryFn(opts)
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

	now := time.Now()
	nowUnix := strconv.FormatInt(now.Unix(), 10)
	nowMS := strconv.FormatInt(now.UnixMilli(), 10)
	tags := []struct {
		Key   string
		Value string
	}{
		{Key: "@amux", Value: "1"},
		{Key: "@amux_workspace", Value: string(wsID)},
		{Key: "@amux_tab", Value: tabID},
		{Key: "@amux_type", Value: "terminal"},
		{Key: "@amux_assistant", Value: "terminal"},
		{Key: "@amux_created_at", Value: nowUnix},
		{Key: "@amux_instance", Value: "cli"},
		{Key: tmux.TagSessionOwner, Value: "cli"},
		{Key: tmux.TagSessionLeaseAt, Value: nowMS},
	}
	for _, tag := range tags {
		if err := tmuxSetSessionTag(sessionName, tag.Key, tag.Value, opts); err != nil {
			if killErr := tmuxKillSession(sessionName, opts); killErr != nil {
				slog.Debug("best-effort session kill failed", "session", sessionName, "error", killErr)
			}
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
