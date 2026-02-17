package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

var (
	tmuxActiveAgentSessionsByActivity = tmux.ActiveAgentSessionsByActivity
	tmuxSessionsWithTags              = tmux.SessionsWithTags
)

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

	stopped := []string{}
	stoppedAgentIDs := []string{}
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
		if sessionType != "agent" {
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
