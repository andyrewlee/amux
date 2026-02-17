package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/validation"
)

func cmdAgentRun(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux agent run --workspace <id> --assistant <name> [--prompt <text>] [--idempotency-key <key>] [--json]"
	fs := newFlagSet("agent run")
	wsFlag := fs.String("workspace", "", "workspace ID (required)")
	assistant := fs.String("assistant", "", "assistant name (required)")
	name := fs.String("name", "", "tab name")
	prompt := fs.String("prompt", "", "initial prompt to send")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for safe retries")
	if err := fs.Parse(args); err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if fs.NArg() > 0 {
		return returnUsageError(
			w, wErr, gf, usage, version,
			fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " ")),
		)
	}
	assistantName := strings.ToLower(strings.TrimSpace(*assistant))
	if *wsFlag == "" || assistantName == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if err := validation.ValidateAssistant(assistantName); err != nil {
		return returnUsageError(
			w,
			wErr,
			gf,
			usage,
			version,
			fmt.Errorf("invalid --assistant: %w", err),
		)
	}
	wsID, err := parseWorkspaceIDFlag(*wsFlag)
	if err != nil {
		return returnUsageError(
			w,
			wErr,
			gf,
			usage,
			version,
			err,
		)
	}
	if handled, code := maybeReplayIdempotentResponse(
		w, wErr, gf, version, "agent.run", *idempotencyKey,
	); handled {
		return code
	}

	svc, err := NewServices(version)
	if err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.run", *idempotencyKey,
				ExitInternalError, "init_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to initialize: %v", err)
		return ExitInternalError
	}

	ws, err := svc.Store.Load(wsID)
	if err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.run", *idempotencyKey,
				ExitNotFound, "not_found", fmt.Sprintf("workspace %s not found", wsID), nil,
			)
		}
		Errorf(wErr, "workspace %s not found", wsID)
		return ExitNotFound
	}

	agentAssistant := assistantName
	ac, ok := svc.Config.Assistants[agentAssistant]
	if !ok {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.run", *idempotencyKey,
				ExitUsage, "unknown_assistant", "unknown assistant: "+agentAssistant, nil,
			)
		}
		Errorf(wErr, "unknown assistant: %s", agentAssistant)
		return ExitUsage
	}

	// Generate tab ID and session name.
	tabID := newAgentTabID()
	sessionName := tmux.SessionName("amux", string(wsID), tabID)

	// Create detached tmux session
	createArgs := []string{
		"new-session", "-d", "-s", sessionName, "-c", ws.Root, ac.Command,
	}
	cmd, cancel := tmuxStartSession(svc.TmuxOpts, createArgs...)
	defer cancel()
	if err := cmd.Run(); err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.run", *idempotencyKey,
				ExitInternalError, "session_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to create tmux session: %v", err)
		return ExitInternalError
	}

	// Tag the session.
	tags := []struct {
		Key   string
		Value string
	}{
		{Key: "@amux", Value: "1"},
		{Key: "@amux_workspace", Value: string(wsID)},
		{Key: "@amux_tab", Value: tabID},
		{Key: "@amux_type", Value: "agent"},
		{Key: "@amux_assistant", Value: agentAssistant},
		{Key: "@amux_created_at", Value: strconv.FormatInt(time.Now().Unix(), 10)},
	}
	for _, tag := range tags {
		if err := tmuxSetSessionTag(sessionName, tag.Key, tag.Value, svc.TmuxOpts); err != nil {
			_ = tmuxKillSession(sessionName, svc.TmuxOpts)
			if gf.JSON {
				return returnJSONErrorMaybeIdempotent(
					w, wErr, gf, version, "agent.run", *idempotencyKey,
					ExitInternalError, "session_tag_failed", err.Error(), map[string]any{
						"session_name": sessionName,
						"tag":          tag.Key,
					},
				)
			}
			Errorf(wErr, "failed to tag session %s (%s): %v", sessionName, tag.Key, err)
			return ExitInternalError
		}
	}

	if code := verifyStartedAgentSession(
		w, wErr, gf, version, *idempotencyKey, sessionName, svc.TmuxOpts,
	); code != ExitOK {
		return code
	}

	if code := sendAgentRunPromptIfRequested(
		w, wErr, gf, version, *idempotencyKey, sessionName, *prompt, svc.TmuxOpts,
	); code != ExitOK {
		return code
	}

	// Persist the tab append atomically to avoid lost updates when multiple
	// agent runs complete concurrently for the same workspace.
	tabName := agentAssistant
	if *name != "" {
		tabName = *name
	}
	tab := data.TabInfo{
		Assistant:   agentAssistant,
		Name:        tabName,
		SessionName: sessionName,
		Status:      "running",
		CreatedAt:   time.Now().Unix(),
	}
	if err := appendWorkspaceOpenTabMeta(svc.Store, wsID, tab); err != nil {
		_ = tmuxKillSession(sessionName, svc.TmuxOpts)
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.run", *idempotencyKey,
				ExitInternalError, "metadata_save_failed", err.Error(), map[string]any{
					"workspace_id": string(wsID),
					"session_name": sessionName,
				},
			)
		}
		Errorf(wErr, "failed to persist workspace metadata: %v", err)
		return ExitInternalError
	}

	result := agentRunResult{
		SessionName: sessionName,
		AgentID:     formatAgentID(string(wsID), tabID),
		WorkspaceID: string(wsID),
		Assistant:   agentAssistant,
		TabID:       tabID,
	}

	if gf.JSON {
		return returnJSONSuccessWithIdempotency(
			w, wErr, gf, version, "agent.run", *idempotencyKey, result,
		)
	}

	PrintHuman(w, func(w io.Writer) {
		fmt.Fprintf(w, "Started agent %s (session: %s)\n", agentAssistant, sessionName)
	})
	return ExitOK
}
