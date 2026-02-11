package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

var (
	tmuxActiveAgentSessionsByActivity = tmux.ActiveAgentSessionsByActivity
	tmuxSessionsWithTags              = tmux.SessionsWithTags
)

type agentStopResult struct {
	Stopped         []string `json:"stopped"`
	AgentID         string   `json:"agent_id,omitempty"`
	StoppedAgentIDs []string `json:"stopped_agent_ids,omitempty"`
}

func cmdAgentStop(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux agent stop (<session_name>|--agent <agent_id>) [--graceful] [--grace-period <dur>] [--idempotency-key <key>] [--json]\n       amux agent stop --all --yes [--graceful] [--grace-period <dur>] [--idempotency-key <key>] [--json]"
	fs := newFlagSet("agent stop")
	all := fs.Bool("all", false, "stop all agents")
	yes := fs.Bool("yes", false, "confirm (required for --all)")
	agentID := fs.String("agent", "", "agent ID (workspace_id:tab_id)")
	graceful := fs.Bool("graceful", true, "send Ctrl-C first and wait before force stop")
	gracePeriod := fs.Duration("grace-period", 1200*time.Millisecond, "wait time before force stop")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for safe retries")
	sessionName, err := parseSinglePositionalWithFlags(fs, args)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if *gracePeriod < 0 {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if *all && (sessionName != "" || *agentID != "") {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}

	if *all {
		if !*yes {
			if gf.JSON {
				return returnJSONErrorMaybeIdempotent(
					w, wErr, gf, version, "agent.stop.all", *idempotencyKey,
					ExitUnsafeBlocked, "confirmation_required", "pass --yes to confirm stopping all agents", nil,
				)
			}
			Errorf(wErr, "pass --yes to confirm stopping all agents")
			return ExitUnsafeBlocked
		}
		if handled, code := maybeReplayIdempotentResponse(
			w, wErr, gf, version, "agent.stop.all", *idempotencyKey,
		); handled {
			return code
		}
		svc, err := NewServices(version)
		if err != nil {
			if gf.JSON {
				return returnJSONErrorMaybeIdempotent(
					w, wErr, gf, version, "agent.stop.all", *idempotencyKey,
					ExitInternalError, "init_failed", err.Error(), nil,
				)
			}
			Errorf(wErr, "failed to initialize: %v", err)
			return ExitInternalError
		}
		return stopAllAgents(
			w, wErr, gf, svc, version, "agent.stop.all", *idempotencyKey, *graceful, *gracePeriod,
		)
	}
	if sessionName == "" && *agentID == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if sessionName != "" && *agentID != "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if handled, code := maybeReplayIdempotentResponse(
		w, wErr, gf, version, "agent.stop", *idempotencyKey,
	); handled {
		return code
	}
	svc, err := NewServices(version)
	if err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.stop", *idempotencyKey,
				ExitInternalError, "init_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to initialize: %v", err)
		return ExitInternalError
	}
	if *agentID != "" {
		resolved, err := resolveSessionNameForAgentID(*agentID, svc.TmuxOpts)
		if err != nil {
			if errors.Is(err, errInvalidAgentID) {
				if gf.JSON {
					return returnJSONErrorMaybeIdempotent(
						w, wErr, gf, version, "agent.stop", *idempotencyKey,
						ExitUsage, "invalid_agent_id", err.Error(), map[string]any{"agent_id": *agentID},
					)
				}
				Errorf(wErr, "invalid --agent: %v", err)
				return ExitUsage
			}
			if errors.Is(err, errAgentNotFound) {
				if gf.JSON {
					return returnJSONErrorMaybeIdempotent(
						w, wErr, gf, version, "agent.stop", *idempotencyKey,
						ExitNotFound, "not_found", "agent not found", map[string]any{"agent_id": *agentID},
					)
				}
				Errorf(wErr, "agent %s not found", *agentID)
				return ExitNotFound
			}
			if gf.JSON {
				return returnJSONErrorMaybeIdempotent(
					w, wErr, gf, version, "agent.stop", *idempotencyKey,
					ExitInternalError, "stop_failed", err.Error(), map[string]any{"agent_id": *agentID},
				)
			}
			Errorf(wErr, "failed to resolve --agent %s: %v", *agentID, err)
			return ExitInternalError
		}
		sessionName = resolved
	}

	state, err := tmuxSessionStateFor(sessionName, svc.TmuxOpts)
	if err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.stop", *idempotencyKey,
				ExitInternalError, "stop_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to check session: %v", err)
		return ExitInternalError
	}
	if !state.Exists {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.stop", *idempotencyKey,
				ExitNotFound, "not_found", fmt.Sprintf("session %s not found", sessionName), nil,
			)
		}
		Errorf(wErr, "session %s not found", sessionName)
		return ExitNotFound
	}

	if err := stopAgentSession(sessionName, svc, *graceful, *gracePeriod); err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.stop", *idempotencyKey,
				ExitInternalError, "stop_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to stop session: %v", err)
		return ExitInternalError
	}

	removeTabFromStore(svc, sessionName)

	result := agentStopResult{Stopped: []string{sessionName}, AgentID: *agentID}

	if gf.JSON {
		return returnJSONSuccessWithIdempotency(
			w, wErr, gf, version, "agent.stop", *idempotencyKey, result,
		)
	}

	PrintHuman(w, func(w io.Writer) {
		fmt.Fprintf(w, "Stopped %s\n", sessionName)
	})
	return ExitOK
}

func stopAllAgents(
	w io.Writer,
	wErr io.Writer,
	gf GlobalFlags,
	svc *Services,
	version string,
	command string,
	idempotencyKey string,
	graceful bool,
	gracePeriod time.Duration,
) int {
	sessions, err := listAgentSessionsForStopAll(svc.TmuxOpts)
	if err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, command, idempotencyKey,
				ExitInternalError, "list_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to list agents: %v", err)
		return ExitInternalError
	}

	var stopped []string
	var stoppedAgentIDs []string
	var failed []map[string]string
	for _, s := range sessions {
		if err := stopAgentSession(s.Name, svc, graceful, gracePeriod); err != nil {
			failed = append(failed, map[string]string{
				"session": s.Name,
				"error":   err.Error(),
			})
			continue
		}
		stopped = append(stopped, s.Name)
		if id := formatAgentID(s.WorkspaceID, s.TabID); id != "" {
			stoppedAgentIDs = append(stoppedAgentIDs, id)
		}
		removeTabFromStore(svc, s.Name)
	}
	if len(failed) > 0 {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, command, idempotencyKey,
				ExitInternalError, "stop_partial_failed", "failed to stop one or more agents", map[string]any{
					"stopped":           stopped,
					"stopped_agent_ids": stoppedAgentIDs,
					"failed":            failed,
				},
			)
		}
		for _, failure := range failed {
			Errorf(wErr, "failed to stop %s: %s", failure["session"], failure["error"])
		}
		PrintHuman(w, func(w io.Writer) {
			fmt.Fprintf(w, "Stopped %d agent(s); %d failed\n", len(stopped), len(failed))
		})
		return ExitInternalError
	}

	result := agentStopResult{Stopped: stopped, StoppedAgentIDs: stoppedAgentIDs}

	if gf.JSON {
		return returnJSONSuccessWithIdempotency(
			w, wErr, gf, version, command, idempotencyKey, result,
		)
	}

	PrintHuman(w, func(w io.Writer) {
		fmt.Fprintf(w, "Stopped %d agent(s)\n", len(stopped))
	})
	return ExitOK
}

func listAgentSessionsForStopAll(opts tmux.Options) ([]tmux.SessionActivity, error) {
	byName := map[string]tmux.SessionActivity{}

	activitySessions, err := tmuxActiveAgentSessionsByActivity(0, opts)
	if err != nil {
		return nil, err
	}
	for _, session := range activitySessions {
		byName[session.Name] = session
	}

	tagged, err := tmuxSessionsWithTags(
		map[string]string{"@amux": "1"},
		[]string{"@amux_workspace", "@amux_tab", "@amux_type"},
		opts,
	)
	if err != nil {
		return nil, err
	}
	for _, row := range tagged {
		sessionType := strings.TrimSpace(row.Tags["@amux_type"])
		// Preserve compatibility with legacy/partially tagged sessions that may
		// not have @amux_type yet.
		if sessionType != "" && sessionType != "agent" {
			continue
		}
		session := byName[row.Name]
		session.Name = row.Name
		if session.WorkspaceID == "" {
			session.WorkspaceID = strings.TrimSpace(row.Tags["@amux_workspace"])
		}
		if session.TabID == "" {
			session.TabID = strings.TrimSpace(row.Tags["@amux_tab"])
		}
		if session.Type == "" {
			session.Type = sessionType
		}
		session.Tagged = true
		byName[row.Name] = session
	}

	if len(byName) == 0 {
		return nil, nil
	}
	sessions := make([]tmux.SessionActivity, 0, len(byName))
	for _, session := range byName {
		sessions = append(sessions, session)
	}
	return sessions, nil
}

func stopAgentSession(sessionName string, svc *Services, graceful bool, gracePeriod time.Duration) error {
	if !graceful {
		return tmuxKillSession(sessionName, svc.TmuxOpts)
	}
	if err := tmuxSendInterrupt(sessionName, svc.TmuxOpts); err != nil {
		return tmuxKillSession(sessionName, svc.TmuxOpts)
	}
	if gracePeriod <= 0 {
		return tmuxKillSession(sessionName, svc.TmuxOpts)
	}

	deadline := time.Now().Add(gracePeriod)
	for time.Now().Before(deadline) {
		state, err := tmuxSessionStateFor(sessionName, svc.TmuxOpts)
		if err == nil && !state.Exists {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return tmuxKillSession(sessionName, svc.TmuxOpts)
}

func removeTabFromStore(svc *Services, sessionName string) {
	ids, err := svc.Store.List()
	if err != nil {
		return
	}
	for _, id := range ids {
		ws, err := svc.Store.Load(id)
		if err != nil {
			continue
		}
		changed := false
		var tabs []data.TabInfo
		for _, tab := range ws.OpenTabs {
			if tab.SessionName == sessionName {
				changed = true
				continue
			}
			tabs = append(tabs, tab)
		}
		if changed {
			ws.OpenTabs = tabs
			_ = svc.Store.Save(ws)
			return
		}
	}
}
