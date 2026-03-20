package cli

import (
	"fmt"
	"strings"
)

func (runner assistantDXRunner) archivedWorkspaceListProbe() ([]map[string]any, string) {
	env, state := runner.invokeQuiet("workspace", "list", "--archived", "--all")
	if state == assistantDXProbeOK {
		return assistantDXArchivedRowsWithDefault(assistantDXArray(env.Data)), assistantDXProbeOK
	}
	if state == assistantDXProbeUnsupportedArchive {
		return []map[string]any{}, state
	}
	firstState := state
	if env, secondState := runner.invokeQuiet("workspace", "list", "--archived"); secondState == assistantDXProbeOK {
		return assistantDXArchivedRowsWithDefault(assistantDXArray(env.Data)), assistantDXProbeOK
	} else if firstState == assistantDXProbeUnsupportedAll {
		switch secondState {
		case assistantDXProbeUnsupportedArchive, assistantDXProbeUnsupported:
			return []map[string]any{}, secondState
		default:
			return []map[string]any{}, firstState
		}
	}
	return []map[string]any{}, firstState
}

func (runner assistantDXRunner) visibleWorkspaceListProbe(commandName string, includeStale bool) (*Envelope, string, *assistantDXPayload) {
	if !includeStale {
		env, errPayload := runner.invokeOK(commandName, "workspace", "list")
		if errPayload != nil {
			return nil, assistantDXProbeError, errPayload
		}
		return env, assistantDXProbeOK, nil
	}

	env, state := runner.invokeQuiet("workspace", "list", "--all")
	switch state {
	case assistantDXProbeOK:
		return env, state, nil
	case assistantDXProbeUnsupportedAll:
		fallbackEnv, errPayload := runner.invokeOK(commandName, "workspace", "list")
		if errPayload != nil {
			return nil, state, errPayload
		}
		return fallbackEnv, state, nil
	default:
		probedEnv, errPayload := runner.invokeOK(commandName, "workspace", "list", "--all")
		if errPayload != nil {
			return nil, state, errPayload
		}
		return probedEnv, assistantDXProbeOK, nil
	}
}

func (runner assistantDXRunner) status(args []string) assistantDXPayload {
	workspace := ""
	assistant := "codex"
	includeStale := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--workspace", "--assistant":
			if i+1 >= len(args) {
				return assistantDXErrorPayload("status", "missing value for "+args[i], "")
			}
			if args[i] == "--workspace" {
				workspace = args[i+1]
			} else {
				assistant = args[i+1]
			}
			i++
		case "--include-stale":
			includeStale = true
		case "--project", "--limit", "--capture-lines", "--capture-agents", "--older-than", "--recent-workspaces":
			if i+1 >= len(args) {
				return assistantDXErrorPayload("status", "missing value for "+args[i], "")
			}
			i++
		case "--alerts-only":
			// Ignored compatibility flag.
		default:
			return assistantDXErrorPayload("status", "unknown flag: "+args[i], "")
		}
	}

	if strings.TrimSpace(workspace) != "" {
		env, errPayload := runner.invokeOK("status", "task", "status", "--workspace", workspace, "--assistant", assistant)
		if errPayload != nil {
			return *errPayload
		}
		taskData := assistantDXObject(env.Data)
		taskStatus := assistantDXFieldString(taskData, "status")
		overall := assistantDXFieldString(taskData, "overall_status")
		status := assistantDXMapTaskStatus(taskStatus, overall)
		summary := assistantDXFieldString(taskData, "summary")
		if summary == "" {
			summary = "Workspace status captured."
		}
		nextAction := assistantDXFieldString(taskData, "next_action")
		if nextAction == "" {
			nextAction = "Continue with the next focused step."
		}
		suggested, actions := runner.buildTaskFollowups(
			workspace,
			assistant,
			taskStatus,
			overall,
			"",
			assistantDXFieldString(taskData, "input_hint"),
			assistantDXFieldString(taskData, "agent_id"),
			"",
			"",
		)
		message := summary + "\nWorkspace: " + workspace + "\nNext: " + nextAction
		return assistantDXBuildPayload(
			true,
			"status",
			status,
			summary,
			nextAction,
			suggested,
			map[string]any{
				"workspace":     workspace,
				"assistant":     assistant,
				"include_stale": includeStale,
				"task":          taskData,
			},
			actions,
			message,
		)
	}

	wsEnv, wsProbeState, errPayload := runner.visibleWorkspaceListProbe("status", includeStale)
	if errPayload != nil {
		return *errPayload
	}
	archivedRows, archivedProbeState := runner.archivedWorkspaceListProbe()
	sessionEnv, sessionErr := runner.invokeOK("status", "session", "list")
	if sessionErr != nil {
		return *sessionErr
	}

	wsRows := assistantDXArray(wsEnv.Data)
	sessionRows := assistantDXArray(sessionEnv.Data)
	archivedOnlyRows := assistantDXArchivedOnlyRows(archivedRows)
	agentRows := assistantDXFilterAgentSessions(sessionRows)

	var visibleWorkspaceRows []map[string]any
	switch {
	case includeStale:
		if archivedProbeState == assistantDXProbeOK {
			visibleWorkspaceRows = assistantDXMergeWorkspaceRows(wsRows, archivedRows)
		} else {
			visibleWorkspaceRows = wsRows
		}
	case archivedProbeState != assistantDXProbeOK:
		visibleWorkspaceRows = wsRows
	default:
		liveAgentIDs := assistantDXUniqueIDs(agentRows, assistantDXSessionWorkspaceID)
		matchingArchivedRows := make([]map[string]any, 0, len(archivedOnlyRows))
		for _, row := range archivedOnlyRows {
			if assistantDXContainsID(liveAgentIDs, assistantDXWorkspaceID(row)) {
				matchingArchivedRows = append(matchingArchivedRows, row)
			}
		}
		visibleWorkspaceRows = assistantDXMergeWorkspaceRows(wsRows, matchingArchivedRows)
	}

	total := assistantDXUnionWorkspaceCount(visibleWorkspaceRows, agentRows)
	agentSessions := len(agentRows)
	firstWorkspaceID := assistantDXFirstWorkspaceID(wsRows)
	firstVisibleWorkspaceID := assistantDXFirstWorkspaceID(visibleWorkspaceRows)
	liveWorkspaceIDs := assistantDXUniqueIDs(wsRows, assistantDXWorkspaceID)
	visibleWorkspaceIDs := assistantDXUniqueIDs(visibleWorkspaceRows, assistantDXWorkspaceID)
	firstLiveAgentWorkspaceID := assistantDXFirstAgentWorkspaceID(agentRows, liveWorkspaceIDs)
	firstVisibleAgentWorkspaceID := assistantDXFirstAgentWorkspaceID(agentRows, visibleWorkspaceIDs)
	orphanedAgentWorkspaceID := assistantDXFirstOrphanedAgentWorkspaceID(agentRows, visibleWorkspaceIDs)

	summary := fmt.Sprintf("%d workspace(s), %d agent session(s).", total, agentSessions)
	nextAction := ""
	suggested := ""
	actions := []assistantDXQuickAction{}
	workspaceListSuggested := assistantDXQuote(runner.selfScript) + " workspace list --all"
	workspaceListDescription := "List all workspaces"
	if includeStale && wsProbeState == assistantDXProbeUnsupportedAll {
		workspaceListSuggested = assistantDXQuote(runner.selfScript) + " workspace list"
		workspaceListDescription = "List visible workspaces"
	}

	switch {
	case agentSessions > 0:
		nextAction = "Check status on the target workspace."
		switch {
		case firstLiveAgentWorkspaceID != "":
			suggested = fmt.Sprintf(
				"%s status --workspace %s --assistant %s",
				assistantDXQuote(runner.selfScript),
				assistantDXQuote(firstLiveAgentWorkspaceID),
				assistantDXQuote(assistant),
			)
			actions = append(actions, assistantDXNewAction("status", "Status", suggested, "primary", "Open active agent workspace status"))
		case firstVisibleAgentWorkspaceID != "":
			suggested = fmt.Sprintf(
				"%s status --workspace %s --assistant %s",
				assistantDXQuote(runner.selfScript),
				assistantDXQuote(firstVisibleAgentWorkspaceID),
				assistantDXQuote(assistant),
			)
			actions = append(actions, assistantDXNewAction("status", "Status", suggested, "primary", "Open active agent workspace status"))
		case orphanedAgentWorkspaceID != "":
			switch archivedProbeState {
			case assistantDXProbeUnsupportedAll, assistantDXProbeUnsupportedArchive, assistantDXProbeUnsupported:
				if firstWorkspaceID == "" {
					suggested = assistantDXQuote(runner.invoker.suggestedAMUXBin()) + " session list"
					actions = append(actions, assistantDXNewAction("sessions", "Sessions", suggested, "primary", "List sessions"))
				} else {
					suggested = assistantDXQuote(runner.selfScript) + " workspace list"
					actions = append(actions, assistantDXNewAction("workspaces", "Workspaces", suggested, "primary", "List visible workspaces"))
				}
			default:
				suggested = workspaceListSuggested
				actions = append(actions, assistantDXNewAction("workspaces", "Workspaces", suggested, "primary", workspaceListDescription))
			}
		case firstVisibleWorkspaceID != "":
			suggested = fmt.Sprintf(
				"%s status --workspace %s --assistant %s",
				assistantDXQuote(runner.selfScript),
				assistantDXQuote(firstVisibleWorkspaceID),
				assistantDXQuote(assistant),
			)
			actions = append(actions, assistantDXNewAction("status", "Status", suggested, "primary", "Open workspace status"))
		default:
			suggested = workspaceListSuggested
			actions = append(actions, assistantDXNewAction("workspaces", "Workspaces", suggested, "primary", workspaceListDescription))
		}
	default:
		nextAction = "Start a bounded task."
		if firstVisibleWorkspaceID != "" {
			suggested = runner.buildTaskStartCmd(firstVisibleWorkspaceID, assistant, "Continue from current state and report status plus next action.", "", "")
			actions = append(actions, assistantDXNewAction("start", "Start", suggested, "primary", "Start task in first workspace"))
		} else {
			suggested = workspaceListSuggested
			actions = append(actions, assistantDXNewAction("workspaces", "Workspaces", suggested, "primary", workspaceListDescription))
		}
	}

	return assistantDXBuildPayload(
		true,
		"status",
		"ok",
		summary,
		nextAction,
		suggested,
		map[string]any{
			"workspaces":     visibleWorkspaceRows,
			"sessions":       sessionRows,
			"include_stale":  includeStale,
			"probe_archived": archivedProbeState,
		},
		actions,
		summary,
	)
}
