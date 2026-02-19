package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

var (
	agentCaptureRetryAttempts = 5
	agentCaptureRetryDelay    = 120 * time.Millisecond
)

type agentInfo struct {
	SessionName string `json:"session_name"`
	AgentID     string `json:"agent_id,omitempty"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	Type        string `json:"type"`
}

type captureResult struct {
	SessionName   string `json:"session_name"`
	Content       string `json:"content"`
	Lines         int    `json:"lines"`
	Status        string `json:"status,omitempty"`
	LatestLine    string `json:"latest_line,omitempty"`
	Summary       string `json:"summary,omitempty"`
	NeedsInput    bool   `json:"needs_input,omitempty"`
	InputHint     string `json:"input_hint,omitempty"`
	SessionExited bool   `json:"session_exited,omitempty"`
}

func cmdAgentList(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux agent list [--workspace <id>] [--json]"
	fs := newFlagSet("agent list")
	workspace := fs.String("workspace", "", "filter by workspace ID")
	if err := fs.Parse(args); err != nil {
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

	sessions, err := tmux.ActiveAgentSessionsByActivity(0, svc.TmuxOpts)
	if err != nil {
		if gf.JSON {
			ReturnError(w, "list_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "failed to list agents: %v", err)
		}
		return ExitInternalError
	}

	agents := []agentInfo{}
	for _, s := range sessions {
		if *workspace != "" && s.WorkspaceID != *workspace {
			continue
		}
		agents = append(agents, agentInfo{
			SessionName: s.Name,
			AgentID:     formatAgentID(s.WorkspaceID, s.TabID),
			WorkspaceID: s.WorkspaceID,
			TabID:       s.TabID,
			Type:        s.Type,
		})
	}

	if gf.JSON {
		PrintJSON(w, agents, version)
		return ExitOK
	}

	PrintHuman(w, func(w io.Writer) {
		if len(agents) == 0 {
			fmt.Fprintln(w, "No running agents.")
			return
		}
		for _, a := range agents {
			if a.AgentID != "" {
				fmt.Fprintf(w, "  %-40s id=%-24s ws=%-16s tab=%-10s type=%s\n",
					a.SessionName, a.AgentID, a.WorkspaceID, a.TabID, a.Type)
				continue
			}
			fmt.Fprintf(w, "  %-40s ws=%-16s tab=%-10s type=%s\n",
				a.SessionName, a.WorkspaceID, a.TabID, a.Type)
		}
	})
	return ExitOK
}

func cmdAgentCapture(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux agent capture <session_name> [--lines N] [--json]"
	fs := newFlagSet("agent capture")
	lines := fs.Int("lines", 50, "number of lines to capture")
	sessionName, err := parseSinglePositionalWithFlags(fs, args)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}

	if sessionName == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}

	if *lines <= 0 {
		if gf.JSON {
			ReturnError(w, "invalid_lines", "--lines must be > 0",
				map[string]any{"lines": *lines}, version)
		} else {
			Errorf(wErr, "--lines must be > 0")
		}
		return ExitUsage
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

	content, ok := captureAgentPaneWithRetry(sessionName, *lines, svc.TmuxOpts)
	if !ok {
		state, stateErr := tmuxSessionStateFor(sessionName, svc.TmuxOpts)
		if stateErr == nil && !state.Exists {
			if gf.JSON {
				result := captureResult{
					SessionName:   sessionName,
					Content:       "",
					Lines:         *lines,
					Status:        "session_exited",
					Summary:       "Agent session exited before capture.",
					SessionExited: true,
				}
				PrintJSON(w, result, version)
				return ExitOK
			}
			Errorf(wErr, "agent session %s has exited", sessionName)
			return ExitNotFound
		}
		if gf.JSON {
			ReturnError(w, "capture_failed", "could not capture pane output", nil, version)
		} else {
			Errorf(wErr, "could not capture pane output for session %s", sessionName)
		}
		return ExitNotFound
	}

	latestLine := latestLineForContent(content)
	needsInput, inputHint := detectNeedsInput(content)
	result := captureResult{
		SessionName: sessionName,
		Content:     content,
		Lines:       *lines,
		Status:      "captured",
		LatestLine:  latestLine,
		Summary:     summarizeWaitResponse("idle", latestLine, needsInput, inputHint),
		NeedsInput:  needsInput,
		InputHint:   inputHint,
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

func captureAgentPaneWithRetry(
	sessionName string,
	lines int,
	opts tmux.Options,
) (string, bool) {
	attempts := agentCaptureRetryAttempts
	if attempts < 1 {
		attempts = 1
	}
	for i := 0; i < attempts; i++ {
		content, ok := tmuxCapturePaneTail(sessionName, lines, opts)
		if ok {
			return content, true
		}
		if i < attempts-1 && agentCaptureRetryDelay > 0 {
			time.Sleep(agentCaptureRetryDelay)
		}
	}
	return "", false
}
