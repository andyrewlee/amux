package app

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// workspaceStatus is the operational snapshot the status dialog renders —
// one read of each existing API, assembled once at open. Nothing here is
// new state: it aggregates what the allocator, run sessions, script config,
// trust registry, env layers, and tab model already know.
type workspaceStatus struct {
	name, branch, root, project string
	shelved, archived           bool
	archivedAt                  string

	portBase, portEnd int
	portAllocated     bool

	runAlive    bool
	runLastExit int

	// scriptSources maps each lifecycle script to where its command comes
	// from ("repo", "user") — absent when unconfigured. Repo scripts also
	// carry the trust gate's verdict separately.
	scriptSources map[process.ScriptType]string
	repoConfig    bool
	repoTrusted   bool

	// envKeys are the merged custom-env key NAMES (repo + project +
	// workspace layers), sorted — names only, never values (the plan-034
	// rule: values can hold secrets).
	envKeys          []string
	envKeySource     map[string]string // key -> "repo"/"project"/"workspace"
	lifecycleOutputs []process.ScriptType
	tabsOpen         int
}

// handleShowWorkspaceStatus opens the read-only status dialog for the
// workspace — a snapshot of the operational state the rest of the app
// scatters across surfaces. Reads are one-shot at open (matching R's
// run-output open); the dialog does not refresh.
//
// The run-session status read is a tmux subprocess — it runs inside the
// returned cmd, off the Update loop (same defect class as the R-open path;
// a wedged tmux would otherwise freeze the whole TUI for a timeout).
func (a *App) handleShowWorkspaceStatus(msg messages.ShowWorkspaceStatus) tea.Cmd {
	ws := msg.Workspace
	if ws == nil || a.workspaceService == nil {
		return nil
	}
	a.overlays.runOutputToken++
	token, svc := a.overlays.runOutputToken, a.workspaceService
	return func() tea.Msg {
		alive, lastExit := svc.RunScriptStatus(ws)
		return workspaceStatusReadyMsg{token: token, ws: ws, runAlive: alive, runLastExit: lastExit}
	}
}

// workspaceStatusReadyMsg delivers the off-loop portion of the status
// snapshot — the run-session read. Everything else buildWorkspaceStatus
// gathers is cheap in-memory/file state applied back on the loop.
type workspaceStatusReadyMsg struct {
	token       int
	ws          *data.Workspace
	runAlive    bool
	runLastExit int
}

// handleWorkspaceStatusReady applies the fetched runner status under the
// shared dialog token — a stale read (a later dialog open bumped it) is
// dropped, not applied.
func (a *App) handleWorkspaceStatusReady(msg workspaceStatusReadyMsg) tea.Cmd {
	if msg.token != a.overlays.runOutputToken || msg.ws == nil {
		return nil
	}
	st := a.buildWorkspaceStatus(msg.ws)
	st.runAlive = msg.runAlive
	st.runLastExit = msg.runLastExit
	content := renderWorkspaceStatus(st)
	a.requestRunOutputOpen(func() {
		a.overlays.runOutputWorkspace = msg.ws
		a.overlays.runOutputAttachable = false
		a.overlays.runOutput = common.NewOutputDialog("Workspace — "+msg.ws.Name, content)
		a.overlays.runOutput.SetSize(a.width, a.height)
		a.overlays.runOutput.Show()
	})
	return nil
}

// buildWorkspaceStatus reads every operational surface once. Each read is
// nil-safe: a missing runner/store/center degrades to the field's empty
// value rather than failing the whole panel.
func (a *App) buildWorkspaceStatus(ws *data.Workspace) workspaceStatus {
	st := workspaceStatus{
		name:        ws.Name,
		branch:      ws.Branch,
		root:        ws.Root,
		shelved:     ws.Shelved,
		archived:    ws.Archived,
		runLastExit: -1, // -1 = no run-session record, matching RunScriptStatus
	}
	if !ws.ArchivedAt.IsZero() {
		st.archivedAt = ws.ArchivedAt.Format("2006-01-02 15:04")
	}
	if a.activeProject != nil {
		st.project = a.activeProject.Name
	}
	st.scriptSources = map[process.ScriptType]string{}
	st.envKeySource = map[string]string{}

	if a.workspaceService != nil {
		a.fillRunnerStatus(&st, ws)
	}
	// User-entered ws.Scripts fill in whatever the repo doesn't define (repo
	// wins per field — the resolveScriptCommand precedence).
	mergeScriptSources(st.scriptSources, ws, "user")
	a.fillEnvSources(&st, ws)
	if a.center != nil {
		if tabs, _ := a.center.GetTabsInfoForWorkspace(string(ws.ID())); len(tabs) > 0 {
			st.tabsOpen = len(tabs)
		}
	}
	return st
}

// fillRunnerStatus reads the port reservation, run-session state, repo
// script config + trust verdict, and recorded lifecycle outputs from the
// runner — extracted to keep buildWorkspaceStatus under the complexity cap.
func (a *App) fillRunnerStatus(st *workspaceStatus, ws *data.Workspace) {
	if base, ok := a.workspaceService.WorkspaceScriptPort(ws); ok {
		st.portBase, st.portAllocated = base, true
		if a.config != nil {
			st.portEnd = base + a.config.PortRangeSize - 1
		}
	}
	// runAlive/runLastExit are filled by handleWorkspaceStatusReady — the
	// tmux status read runs off-loop in the open cmd.
	if cfg, err := a.workspaceService.ScriptConfig(ws.Repo); err == nil && cfg != nil {
		st.repoConfig = len(cfg.SetupWorkspace) > 0 || cfg.RunScript != "" ||
			cfg.ArchiveScript != "" || cfg.OnDoneScript != "" || len(cfg.Env) > 0
		setIf := func(t process.ScriptType, set bool) {
			if set {
				st.scriptSources[t] = "repo"
			}
		}
		setIf(process.ScriptSetup, len(cfg.SetupWorkspace) > 0)
		setIf(process.ScriptRun, cfg.RunScript != "")
		setIf(process.ScriptArchive, cfg.ArchiveScript != "")
		setIf(process.ScriptOnDone, cfg.OnDoneScript != "")
		for k := range cfg.Env {
			st.envKeySource[k] = "repo"
		}
	}
	// Trust verdict is read whether or not the config has scripts —
	// ScriptsTrusted reports true for a config-less repo, so the render
	// can distinguish "no repo config" from "untrusted".
	if trusted, err := a.workspaceService.WorkspaceScriptsTrusted(ws.Repo); err == nil {
		st.repoTrusted = trusted
	}
	for stType, entry := range a.workspaceService.LastScriptOutputs(ws) {
		if entry.Text != "" || entry.Err != "" {
			st.lifecycleOutputs = append(st.lifecycleOutputs, stType)
		}
	}
	sort.Slice(st.lifecycleOutputs, func(i, j int) bool {
		return st.lifecycleOutputs[i] < st.lifecycleOutputs[j]
	})
}

// mergeScriptSources labels each configured script source unless a
// higher-precedence layer already claimed it (repo wins over user).
func mergeScriptSources(dst map[process.ScriptType]string, ws *data.Workspace, src string) {
	for t, cmd := range map[process.ScriptType]string{
		process.ScriptSetup:   ws.Scripts.Setup,
		process.ScriptRun:     ws.Scripts.Run,
		process.ScriptArchive: ws.Scripts.Archive,
		process.ScriptOnDone:  ws.Scripts.OnDone,
	} {
		if cmd != "" {
			if _, ok := dst[t]; !ok {
				dst[t] = src
			}
		}
	}
}

// fillEnvSources merges the env key names from the project and workspace
// layers on top of whatever the repo layer contributed — names only.
func (a *App) fillEnvSources(st *workspaceStatus, ws *data.Workspace) {
	if a.projectEnvStore != nil {
		for k := range a.projectEnvStore.ForRepo(ws.Repo) {
			// A repo-defined key keeps its "repo" source label — it is the
			// lower-precedence layer and the one under a trust gate.
			if _, ok := st.envKeySource[k]; !ok {
				st.envKeySource[k] = "project"
			}
		}
	}
	for k := range ws.Env {
		if _, ok := st.envKeySource[k]; !ok {
			st.envKeySource[k] = "workspace"
		}
	}
	for k := range st.envKeySource {
		st.envKeys = append(st.envKeys, k)
	}
	sort.Strings(st.envKeys)
}

// renderWorkspaceStatus lays the snapshot out as grouped plain text for the
// scrollable OutputDialog — read-only, additive sections only.
func renderWorkspaceStatus(st workspaceStatus) string {
	var b strings.Builder

	writeKV := func(k, v string) {
		fmt.Fprintf(&b, "%-12s %s\n", k+":", v)
	}

	b.WriteString("identity\n")
	writeKV("branch", orDash(st.branch))
	writeKV("path", orDash(st.root))
	if st.project != "" {
		writeKV("project", st.project)
	}
	state := "active"
	switch {
	case st.shelved:
		state = "shelved"
	case st.archived:
		state = "archived " + st.archivedAt
	}
	writeKV("state", state)
	writeKV("open tabs", strconv.Itoa(st.tabsOpen))

	b.WriteString("\nruntime\n")
	if st.portAllocated {
		writeKV("port range", fmt.Sprintf("%d-%d", st.portBase, st.portEnd))
	} else {
		writeKV("port range", "none allocated")
	}
	switch {
	case st.runAlive:
		writeKV("run script", "running")
	case st.runLastExit >= 0:
		writeKV("run script", fmt.Sprintf("stopped (last exit %d)", st.runLastExit))
	default:
		writeKV("run script", "not running")
	}
	if len(st.lifecycleOutputs) > 0 {
		types := make([]string, len(st.lifecycleOutputs))
		for i, t := range st.lifecycleOutputs {
			types[i] = string(t)
		}
		writeKV("recorded", strings.Join(types, ", ")+" — press O")
	}

	b.WriteString("\nscripts\n")
	for _, stType := range []process.ScriptType{
		process.ScriptSetup, process.ScriptRun, process.ScriptArchive, process.ScriptOnDone,
	} {
		src, ok := st.scriptSources[stType]
		if !ok {
			src = "—"
		}
		writeKV(string(stType), src)
	}
	trust := "no repo config"
	if st.repoConfig {
		if st.repoTrusted {
			trust = "trusted"
		} else {
			trust = "NOT trusted — repo scripts gated"
		}
	}
	writeKV("repo trust", trust)

	b.WriteString("\nenv keys\n")
	if len(st.envKeys) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, k := range st.envKeys {
		fmt.Fprintf(&b, "  %-28s %s\n", k, st.envKeySource[k])
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
