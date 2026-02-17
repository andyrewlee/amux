package cli

import (
	"fmt"
	"io"
	"strings"
	"time"
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

	rows, err := svc.QuerySessionRows(svc.TmuxOpts)
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
