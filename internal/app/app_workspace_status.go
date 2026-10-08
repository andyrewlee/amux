package app

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/tmux"
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
	// workspace layers), sorted — names only, never values —
	// values can hold secrets).
	envKeys          []string
	envKeySource     map[string]string // key -> "repo"/"project"/"workspace"
	lifecycleOutputs []process.ScriptType
	tabsOpen         int

	// cleanup is the tombstone probe result — read off-loop in the open
	// cmd (IsDeleting + one os.Stat), not by buildWorkspaceStatus.
	cleanup workspacesvc.WorkspaceCleanup
	// reclaimablePorts counts registry reservations whose owner workspace
	// is provably deleted (absent from every known identity AND owning zero
	// amux sessions). Read-only enumeration — the release stays manual.
	reclaimablePorts int
}

// handleShowWorkspaceStatus opens the read-only status dialog for the
// workspace — a snapshot of the operational state the rest of the app
// scatters across surfaces. Reads are one-shot at open (matching R's
// run-output open); the dialog does not refresh.
//
// The run-session status read is a tmux subprocess — it runs inside the
// returned cmd, off the Update loop (same defect class as the R-open path;
// a wedged tmux would otherwise freeze the whole TUI for a timeout). The port
// interval read is a durable-registry read in production — the same off-loop
// placement applies, and it must NOT allocate: viewing status observes, it
// does not reserve. fetchWorkspaceStatusReads rides the same cmd: the repo
// config, trust registry, and ~/.amux store reads are file I/O of the same
// class, so a wedged filesystem must stall the fetch, not the TUI.
func (a *App) handleShowWorkspaceStatus(msg messages.ShowWorkspaceStatus) tea.Cmd {
	ws := msg.Workspace
	if ws == nil || a.workspaceService == nil {
		return nil
	}
	a.overlays.runOutputToken++
	token, svc := a.overlays.runOutputToken, a.workspaceService
	snap := ws.Clone() // async closure convention — the live model may mutate
	// Live + shelved workspace snapshot for the reclaimable-reservation
	// probe — captured on the loop because it walks a.projects (same
	// convention as the orphan GC's known-IDs set).
	knownIDs := a.collectPortReservationOwnerIDs()
	opts := a.tmuxOptions
	return func() tea.Msg {
		alive, lastExit := svc.RunScriptStatus(&snap)
		base, end, found, portErr := svc.WorkspacePortInterval(&snap)
		// Read-only tombstone probe: IsDeleting + os.Stat, off the Update
		// loop like the tmux/registry reads it rides alongside.
		cleanup := svc.WorkspaceCleanupSnapshot(&snap)
		reclaimable := a.reclaimablePortReservations(svc, knownIDs, opts)
		return workspaceStatusReadyMsg{
			token: token, ws: ws, runAlive: alive, runLastExit: lastExit,
			portBase: base, portEnd: end, portFound: found, portErr: portErr,
			cleanup: cleanup, reclaimablePorts: reclaimable,
			reads: a.fetchWorkspaceStatusReads(&snap),
		}
	}
}

// reclaimablePortReservations counts registry entries whose owner workspace
// is provably deleted: absent from every known workspace identity AND
// owning zero amux-tagged tmux sessions. Both conditions are required —
// anything live-looking stays counted as live. Read failures fail closed to
// zero (a claimed count would overstate reclaimability); the probe releases
// nothing — reclaim stays a separate, deliberate decision.
func (a *App) reclaimablePortReservations(svc *workspacesvc.Service, knownIDs map[string]bool, opts tmux.Options) int {
	return len(a.reclaimablePortReservationIDs(svc, knownIDs, opts))
}

// reclaimablePortReservationIDs is the reclaimablePortReservations probe with
// the orphan set exposed — the release action re-runs it verbatim so the
// released set is always re-derived, never trusted from display time.
func (a *App) reclaimablePortReservationIDs(svc *workspacesvc.Service, knownIDs map[string]bool, opts tmux.Options) []string {
	reserved, err := svc.ReservedPortIntervals()
	if err != nil {
		logging.Warn("status: port reservation snapshot failed: %v", err)
		return nil
	}
	if len(reserved) == 0 {
		return nil
	}
	sessionsByWS, err := a.amuxSessionsByWorkspace(opts)
	if err != nil {
		logging.Warn("status: session listing failed; reservation count suppressed: %v", err)
		return nil
	}
	var orphans []string
	for id := range reserved {
		if knownIDs[id] || len(sessionsByWS[id]) > 0 {
			continue
		}
		orphans = append(orphans, id)
	}
	return orphans
}

// workspaceStatusReadyMsg delivers the off-loop portion of the status
// snapshot — the run-session read, the durable port-interval read, and every
// file-backed store read. buildWorkspaceStatus consumes the prefetched reads
// so the on-loop assembly is pure.
type workspaceStatusReadyMsg struct {
	token            int
	ws               *data.Workspace
	runAlive         bool
	runLastExit      int
	portBase         int
	portEnd          int
	portFound        bool
	portErr          error
	cleanup          workspacesvc.WorkspaceCleanup
	reclaimablePorts int
	reads            workspaceStatusReads
}

// workspaceStatusReads bundles every filesystem/registry read the status
// panel needs. It is produced inside the open cmd — off the Update loop —
// so a slow or wedged filesystem stalls the fetch, never the TUI.
type workspaceStatusReads struct {
	repoCfg        *process.WorkspaceConfig
	repoTrusted    bool
	repoTrustedOK  bool
	projectScripts data.ScriptsConfig
	projectEnv     map[string]string
}

// fetchWorkspaceStatusReads performs those reads. Errors degrade exactly as
// the synchronous path did: a failed ScriptConfig reads as "no repo config"
// and a failed trust probe leaves repoTrusted unset (repoTrustedOK=false).
func (a *App) fetchWorkspaceStatusReads(ws *data.Workspace) workspaceStatusReads {
	var reads workspaceStatusReads
	if a.workspaceService != nil {
		if cfg, err := a.workspaceService.ScriptConfig(ws.Repo); err == nil && cfg != nil {
			reads.repoCfg = cfg
		}
		// The trust verdict is read whether or not the config has scripts —
		// WorkspaceScriptsTrusted reports true for a config-less repo, so the
		// render can distinguish "no repo config" from "untrusted".
		if trusted, err := a.workspaceService.WorkspaceScriptsTrusted(ws.Repo); err == nil {
			reads.repoTrusted, reads.repoTrustedOK = trusted, true
		}
	}
	if a.projectScriptStore != nil {
		reads.projectScripts = a.projectScriptStore.ForRepo(ws.Repo)
	}
	if a.projectEnvStore != nil {
		reads.projectEnv = a.projectEnvStore.ForRepo(ws.Repo)
	}
	return reads
}

// handleWorkspaceStatusReady applies the fetched runner status under the
// shared dialog token — a stale read (a later dialog open bumped it) is
// dropped, not applied. A failed port read surfaces through the toast path
// rather than opening the dialog with a guessed or blank range.
func (a *App) handleWorkspaceStatusReady(msg workspaceStatusReadyMsg) tea.Cmd {
	if msg.token != a.overlays.runOutputToken || msg.ws == nil {
		return nil
	}
	if msg.portErr != nil {
		return a.toast.ShowError("Cannot read port reservation: " + msg.portErr.Error())
	}
	st := a.buildWorkspaceStatus(msg.ws, msg.reads)
	st.runAlive = msg.runAlive
	st.runLastExit = msg.runLastExit
	st.cleanup = msg.cleanup
	st.reclaimablePorts = msg.reclaimablePorts
	if msg.portFound {
		st.portBase, st.portEnd, st.portAllocated = msg.portBase, msg.portEnd, true
	}
	content := renderWorkspaceStatus(st)
	a.requestRunOutputOpen(func() {
		a.overlays.runOutputWorkspace = msg.ws
		a.overlays.runOutputAttachable = false
		a.overlays.runOutputReleaseCount = st.reclaimablePorts
		// The U intercept is armed only when a grant is actually recorded —
		// the same trusted+config pair the status row advertises.
		a.overlays.runOutputUntrustable = st.repoConfig && st.repoTrusted
		a.overlays.runOutput = common.NewOutputDialog("Workspace — "+msg.ws.Name, content)
		a.overlays.runOutput.SetSize(a.width, a.height)
		a.overlays.runOutput.Show()
	})
	return nil
}

// buildWorkspaceStatus assembles the operational snapshot once. Every
// file-backed read arrives prefetched on reads (produced off-loop by
// fetchWorkspaceStatusReads); what remains here is nil-safe in-memory state —
// a missing runner/store/center degrades to the field's empty value rather
// than failing the whole panel.
func (a *App) buildWorkspaceStatus(ws *data.Workspace, reads workspaceStatusReads) workspaceStatus {
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
		a.fillRunnerStatus(&st, ws, reads)
	}
	// User-entered ws.Scripts fill in whatever the repo doesn't define, then
	// the project defaults file fills whatever remains — the same
	// repo → workspace → project precedence resolveScriptCommand applies.
	mergeScriptSources(st.scriptSources, ws.Scripts, "user")
	mergeScriptSources(st.scriptSources, reads.projectScripts, "project")
	a.fillEnvSources(&st, ws, reads.projectEnv)
	if a.center != nil {
		if tabs, _ := a.center.GetTabsInfoForWorkspace(string(ws.ID())); len(tabs) > 0 {
			st.tabsOpen = len(tabs)
		}
	}
	return st
}

// fillRunnerStatus applies the prefetched repo script config + trust verdict
// and reads recorded lifecycle outputs from the runner — extracted to keep
// buildWorkspaceStatus under the complexity cap. The port range is NOT read
// here: the durable registry read runs off-loop in the open cmd and arrives
// on workspaceStatusReadyMsg, so a stored interval's actual (possibly
// config-divergent) end is what renders.
func (a *App) fillRunnerStatus(st *workspaceStatus, ws *data.Workspace, reads workspaceStatusReads) {
	// runAlive/runLastExit and the port interval are filled by
	// handleWorkspaceStatusReady — the tmux/registry reads run off-loop in
	// the open cmd. So do the repo config and trust reads: they arrive on
	// reads rather than being re-read here.
	if cfg := reads.repoCfg; cfg != nil {
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
	if reads.repoTrustedOK {
		st.repoTrusted = reads.repoTrusted
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
// higher-precedence layer already claimed it (repo wins over workspace wins
// over project). Callers apply the layers in precedence order.
func mergeScriptSources(dst map[process.ScriptType]string, cfg data.ScriptsConfig, src string) {
	for t, cmd := range map[process.ScriptType]string{
		process.ScriptSetup:   cfg.Setup,
		process.ScriptRun:     cfg.Run,
		process.ScriptArchive: cfg.Archive,
		process.ScriptOnDone:  cfg.OnDone,
	} {
		if cmd != "" {
			if _, ok := dst[t]; !ok {
				dst[t] = src
			}
		}
	}
}

// fillEnvSources merges the env key names from the prefetched project map
// and the workspace layer on top of whatever the repo layer contributed —
// names only.
func (a *App) fillEnvSources(st *workspaceStatus, ws *data.Workspace, projectEnv map[string]string) {
	for k := range projectEnv {
		// A repo-defined key keeps its "repo" source label — it is the
		// lower-precedence layer and the one under a trust gate.
		if _, ok := st.envKeySource[k]; !ok {
			st.envKeySource[k] = "project"
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
			trust = "trusted — press U to revoke"
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

	// The cleanup section renders only when a delete tombstone exists, the
	// probe couldn't read identity, or orphaned port reservations were
	// counted — the tombstone is a boolean and the reservation count is
	// read-only, so the text states honest facts rather than fabricating a
	// stage or offering a release action.
	if st.cleanup != workspacesvc.WorkspaceCleanupNone || st.reclaimablePorts > 0 {
		b.WriteString("\ncleanup\n")
		switch st.cleanup {
		case workspacesvc.WorkspaceCleanupInterrupted:
			writeKV("state", "interrupted — worktree still present; workspace remains usable")
			writeKV("detail", `warn-level "workspace delete" log`)
		case workspacesvc.WorkspaceCleanupPending:
			writeKV("state", "pending — worktree already removed; retry automatic on next load")
			writeKV("detail", `warn-level "startup recovery" log`)
		case workspacesvc.WorkspaceCleanupUnknown:
			writeKV("state", "unknown — could not read recovery state")
		}
		if st.reclaimablePorts > 0 {
			writeKV("port reservations", fmt.Sprintf("%d held by deleted workspaces — press R to release", st.reclaimablePorts))
		}
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
