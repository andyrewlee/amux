package cli

import (
	"fmt"
	"os"
	"strings"
)

func (runner assistantDXRunner) projectList(args []string) assistantDXPayload {
	query := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--query":
			if i+1 >= len(args) {
				return assistantDXErrorPayload("project.list", "missing value for --query", "")
			}
			query = args[i+1]
			i++
		case "--limit", "--page":
			if i+1 >= len(args) {
				return assistantDXErrorPayload("project.list", "missing value for "+args[i], "")
			}
			i++
		default:
			return assistantDXErrorPayload("project.list", "unknown flag: "+args[i], "")
		}
	}

	env, errPayload := runner.invokeOK("project.list", "project", "list")
	if errPayload != nil {
		return *errPayload
	}
	projects := assistantDXArray(env.Data)
	filtered := make([]map[string]any, 0, len(projects))
	queryLower := strings.ToLower(strings.TrimSpace(query))
	for _, project := range projects {
		if queryLower == "" {
			filtered = append(filtered, project)
			continue
		}
		search := strings.ToLower(assistantDXFieldString(project, "name") + " " + assistantDXFieldString(project, "path"))
		if strings.Contains(search, queryLower) {
			filtered = append(filtered, project)
		}
	}

	summary := fmt.Sprintf("%d project(s)", len(filtered))
	suggested := assistantDXQuote(runner.selfScript) + " workspace list --all"
	actions := []assistantDXQuickAction{}
	if len(filtered) > 0 {
		firstPath := assistantDXFieldString(filtered[0], "path")
		suggested = fmt.Sprintf("%s workspace list --project %s", assistantDXQuote(runner.selfScript), assistantDXQuote(firstPath))
		actions = append(actions, assistantDXNewAction("workspaces", "Workspaces", suggested, "primary", "List workspaces for first project"))
	} else {
		actions = append(actions, assistantDXNewAction("add", "Add Project", assistantDXQuote(runner.selfScript)+" project add --cwd", "primary", "Register current repo as project"))
	}

	return assistantDXBuildPayload(
		true,
		"project.list",
		"ok",
		summary,
		"Choose project/workspace and start task.",
		suggested,
		map[string]any{"query": query, "projects": filtered},
		actions,
		"✅ "+summary,
	)
}

func (runner assistantDXRunner) projectAdd(args []string) assistantDXPayload {
	path := ""
	useCWD := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--path":
			if i+1 >= len(args) {
				return assistantDXErrorPayload("project.add", "missing value for --path", "")
			}
			path = args[i+1]
			i++
		case "--cwd":
			useCWD = true
		case "--workspace", "--assistant", "--base":
			if i+1 >= len(args) {
				return assistantDXErrorPayload("project.add", "missing value for "+args[i], "")
			}
			i++
		default:
			return assistantDXErrorPayload("project.add", "unknown flag: "+args[i], "")
		}
	}
	if useCWD && strings.TrimSpace(path) == "" {
		if cwd, err := os.Getwd(); err == nil {
			path = cwd
		}
	}
	if strings.TrimSpace(path) == "" {
		return assistantDXErrorPayload("project.add", "missing required flag: --path (or --cwd)", "")
	}
	env, errPayload := runner.invokeOK("project.add", "project", "add", path)
	if errPayload != nil {
		return *errPayload
	}
	return assistantDXBuildPayload(
		true,
		"project.add",
		"ok",
		"Project registered.",
		"Create/list workspace next.",
		fmt.Sprintf("%s workspace list --project %s", assistantDXQuote(runner.selfScript), assistantDXQuote(path)),
		assistantDXObject(env.Data),
		[]assistantDXQuickAction{},
		"✅ Project registered.",
	)
}

func (runner assistantDXRunner) workspaceList(args []string) assistantDXPayload {
	commandArgs := []string{"workspace", "list"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--project", "--repo":
			if i+1 >= len(args) {
				return assistantDXErrorPayload("workspace.list", "missing value for "+args[i], "")
			}
			commandArgs = append(commandArgs, args[i], args[i+1])
			i++
		case "--all", "--archived":
			commandArgs = append(commandArgs, args[i])
		default:
			return assistantDXErrorPayload("workspace.list", "unknown flag: "+args[i], "")
		}
	}
	env, errPayload := runner.invokeOK("workspace.list", commandArgs...)
	if errPayload != nil {
		return *errPayload
	}
	workspaces := assistantDXArray(env.Data)
	summary := fmt.Sprintf("%d workspace(s)", len(workspaces))
	suggested := assistantDXQuote(runner.selfScript) + " project list"
	actions := []assistantDXQuickAction{}
	if firstWorkspaceID := assistantDXFirstWorkspaceID(workspaces); firstWorkspaceID != "" {
		suggested = fmt.Sprintf("%s status --workspace %s", assistantDXQuote(runner.selfScript), assistantDXQuote(firstWorkspaceID))
		actions = append(actions, assistantDXNewAction("status", "Status", suggested, "primary", "Check first workspace status"))
	}
	return assistantDXBuildPayload(
		true,
		"workspace.list",
		"ok",
		summary,
		"Start or continue task in target workspace.",
		suggested,
		map[string]any{"workspaces": workspaces},
		actions,
		"✅ "+summary,
	)
}

func (runner assistantDXRunner) workspaceCreate(args []string) assistantDXPayload {
	name := ""
	project := ""
	assistant := ""
	base := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--project", "--assistant", "--base":
			if i+1 >= len(args) {
				return assistantDXErrorPayload("workspace.create", "missing value for "+args[i], "")
			}
			switch args[i] {
			case "--project":
				project = args[i+1]
			case "--assistant":
				assistant = args[i+1]
			case "--base":
				base = args[i+1]
			}
			i++
		case "--name":
			return assistantDXErrorPayload("workspace.create", "--name is no longer supported; use positional name: workspace create <name> --project <repo>", "")
		case "--from-workspace", "--scope", "--idempotency-key":
			return assistantDXErrorPayload("workspace.create", args[i]+" is not supported by amux workspace create", "")
		default:
			if strings.HasPrefix(args[i], "--") {
				return assistantDXErrorPayload("workspace.create", "unknown flag: "+args[i], "")
			}
			if strings.TrimSpace(name) != "" {
				return assistantDXErrorPayload("workspace.create", "unexpected positional argument: "+args[i], "")
			}
			name = args[i]
		}
	}
	if strings.TrimSpace(name) == "" {
		return assistantDXErrorPayload("workspace.create", "missing required positional argument: <name>", "")
	}
	if strings.TrimSpace(project) == "" {
		return assistantDXErrorPayload("workspace.create", "missing required flag: --project", "")
	}
	commandArgs := []string{"workspace", "create", name, "--project", project}
	if strings.TrimSpace(assistant) != "" {
		commandArgs = append(commandArgs, "--assistant", assistant)
	}
	if strings.TrimSpace(base) != "" {
		commandArgs = append(commandArgs, "--base", base)
	}
	env, errPayload := runner.invokeOK("workspace.create", commandArgs...)
	if errPayload != nil {
		return *errPayload
	}
	data := assistantDXObject(env.Data)
	workspaceID := assistantDXFieldString(data, "id")
	workspaceAssistant := assistantDXFieldString(data, "assistant")
	if workspaceAssistant == "" {
		workspaceAssistant = "codex"
	}
	suggested := assistantDXQuote(runner.selfScript) + " workspace list --all"
	if workspaceID != "" {
		suggested = runner.buildTaskStartCmd(workspaceID, workspaceAssistant, "Continue from current state and report status plus next action.", "", "")
	}
	return assistantDXBuildPayload(
		true,
		"workspace.create",
		"ok",
		"Workspace created.",
		"Start a bounded task.",
		suggested,
		data,
		[]assistantDXQuickAction{},
		"✅ Workspace created.",
	)
}
