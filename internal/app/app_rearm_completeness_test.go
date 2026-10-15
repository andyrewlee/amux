package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// rearmChainClassification is the single registry of how every message type
// dispatched by App.update's top-level switch is classified for panic
// re-arming: chain=true means the type drives a periodic chain and must map
// to a self-fencing starter (proven non-nil in the chain's own tests —
// app-level chains in TestRearmCmdForPanic_MapsPeriodicChains, per-tab PTY
// chains in the models' RearmForPanic tests). chain=false means non-periodic:
// Update's panic path drops the cmds and surfaces a.err, unchanged.
//
// msg carries a zero-value sample so classified-nil rows are also exercised
// against rearmCmdForPanic — a type silently wired to a starter later fails
// the nil assertion below.
var rearmChainClassification = []struct {
	name  string
	msg   tea.Msg
	chain bool
}{
	// update()'s inline switch (app_input.go)
	{"tea.KeyboardEnhancementsMsg", zeroMsg[tea.KeyboardEnhancementsMsg](), false},
	{"tea.WindowSizeMsg", zeroMsg[tea.WindowSizeMsg](), false},
	{"tea.MouseClickMsg", zeroMsg[tea.MouseClickMsg](), false},
	{"tea.MouseWheelMsg", zeroMsg[tea.MouseWheelMsg](), false},
	{"tea.MouseMotionMsg", zeroMsg[tea.MouseMotionMsg](), false},
	{"tea.MouseReleaseMsg", zeroMsg[tea.MouseReleaseMsg](), false},
	{"tea.PasteMsg", zeroMsg[tea.PasteMsg](), false},
	{"app.prefixTimeoutMsg", zeroMsg[prefixTimeoutMsg](), false},
	{"tea.KeyPressMsg", zeroMsg[tea.KeyPressMsg](), false},
	{"center.PTYOutput", zeroMsg[center.PTYOutput](), false},
	{"center.PTYFlush", zeroMsg[center.PTYFlush](), true},
	{"center.PTYStopped", zeroMsg[center.PTYStopped](), true},
	{"messages.Toast", zeroMsg[messages.Toast](), false},
	{"app.frameCoalesceKick", zeroMsg[frameCoalesceKick](), false},
	{"messages.SidebarPTYOutput", zeroMsg[messages.SidebarPTYOutput](), false},
	{"messages.SidebarPTYFlush", zeroMsg[messages.SidebarPTYFlush](), true},
	{"messages.SidebarPTYStopped", zeroMsg[messages.SidebarPTYStopped](), true},
	{"messages.SidebarPTYRestart", zeroMsg[messages.SidebarPTYRestart](), true},
	{"sidebar.SidebarTerminalCreated", zeroMsg[sidebar.SidebarTerminalCreated](), false},
	{"sidebar.SidebarTerminalCreateFailed", zeroMsg[sidebar.SidebarTerminalCreateFailed](), false},
	{"sidebar.SidebarTerminalReattachResult", zeroMsg[sidebar.SidebarTerminalReattachResult](), false},
	{"sidebar.SidebarTerminalReattachFailed", zeroMsg[sidebar.SidebarTerminalReattachFailed](), false},
	{"sidebar.SidebarTabWritten", zeroMsg[sidebar.SidebarTabWritten](), false},
	{"sidebar.SidebarInputFailed", zeroMsg[sidebar.SidebarInputFailed](), false},
	// chain=true is required: the sidebar's stationary edge-hold scroll is a
	// self-rearming chain — only the tick keeps it moving while the pointer
	// sits still, so a panic-dropped tick ends the scroll unless rearmed.
	{"messages.SidebarSelectionScrollTick", zeroMsg[messages.SidebarSelectionScrollTick](), true},
	{"messages.OpenFileInVim", zeroMsg[messages.OpenFileInVim](), false},
	{"messages.BranchChangesLoaded", zeroMsg[messages.BranchChangesLoaded](), false},
	{"messages.AheadBehindLoaded", zeroMsg[messages.AheadBehindLoaded](), false},
	{"sidebar.ProjectTreeDirectoryLoaded", zeroMsg[sidebar.ProjectTreeDirectoryLoaded](), false},
	{"messages.TabSessionStatus", zeroMsg[messages.TabSessionStatus](), false},
	{"messages.Error", zeroMsg[messages.Error](), false},

	// updateTabMsg (app_input_dispatch.go)
	{"messages.OpenDiff", zeroMsg[messages.OpenDiff](), false},
	{"messages.CloseTab", zeroMsg[messages.CloseTab](), false},
	{"messages.LaunchAgent", zeroMsg[messages.LaunchAgent](), false},
	{"messages.TabCreated", zeroMsg[messages.TabCreated](), false},
	{"messages.TabClosed", zeroMsg[messages.TabClosed](), false},
	{"messages.TabDetached", zeroMsg[messages.TabDetached](), false},
	{"messages.TabReattached", zeroMsg[messages.TabReattached](), false},
	{"messages.TabStateChanged", zeroMsg[messages.TabStateChanged](), false},
	{"messages.TabSelectionChanged", zeroMsg[messages.TabSelectionChanged](), false},
	{"app.persistDebounceMsg", zeroMsg[persistDebounceMsg](), false},
	{"app.persistSaveFailedMsg", zeroMsg[persistSaveFailedMsg](), false},
	{"center.TabInputFailed", zeroMsg[center.TabInputFailed](), false},
	{"center.TabInputRejected", zeroMsg[center.TabInputRejected](), false},
	{"center.TabBell", zeroMsg[center.TabBell](), false},

	// updateTmuxMsg (app_input_dispatch.go)
	{"messages.CleanupTmuxSessions", zeroMsg[messages.CleanupTmuxSessions](), false},
	{"dashboard.SpinnerTickMsg", zeroMsg[dashboard.SpinnerTickMsg](), true},
	{"messages.GitStatusTick", zeroMsg[messages.GitStatusTick](), true},
	{"messages.OrphanGCTick", zeroMsg[messages.OrphanGCTick](), true},
	{"messages.PTYWatchdogTick", zeroMsg[messages.PTYWatchdogTick](), true},
	{"app.tmuxActivityTick", zeroMsg[tmuxActivityTick](), true},
	{"app.tmuxActivityResult", zeroMsg[tmuxActivityResult](), false},
	{"app.tmuxAvailableResult", zeroMsg[tmuxAvailableResult](), false},
	{"messages.TmuxSyncTick", zeroMsg[messages.TmuxSyncTick](), true},
	{"app.tmuxTabsSyncResult", zeroMsg[tmuxTabsSyncResult](), false},
	{"app.tmuxTabsDiscoverResult", zeroMsg[tmuxTabsDiscoverResult](), false},
	{"app.tmuxSidebarDiscoverResult", zeroMsg[tmuxSidebarDiscoverResult](), false},
	{"app.orphanGCResult", zeroMsg[orphanGCResult](), false},
	{"app.staleDetachedAgentGCResult", zeroMsg[staleDetachedAgentGCResult](), false},
	{"app.sessionCountResult", zeroMsg[sessionCountResult](), false},

	// updateWorkspaceLifecycleMsg (app_input_dispatch.go)
	{"messages.ProjectsLoaded", zeroMsg[messages.ProjectsLoaded](), false},
	{"messages.WorkspaceActivated", zeroMsg[messages.WorkspaceActivated](), false},
	{"messages.RefreshDashboard", zeroMsg[messages.RefreshDashboard](), false},
	{"messages.RescanWorkspaces", zeroMsg[messages.RescanWorkspaces](), false},
	{"messages.WorkspaceCreatedWithWarning", zeroMsg[messages.WorkspaceCreatedWithWarning](), false},
	{"messages.WorkspaceCreated", zeroMsg[messages.WorkspaceCreated](), false},
	{"messages.WorkspaceSetupComplete", zeroMsg[messages.WorkspaceSetupComplete](), false},
	{"messages.WorkspaceCreateFailed", zeroMsg[messages.WorkspaceCreateFailed](), false},
	{"messages.GitStatusRequest", zeroMsg[messages.GitStatusRequest](), false},
	{"messages.GitStatusResult", zeroMsg[messages.GitStatusResult](), false},
	{"messages.GitStatusBatchResult", zeroMsg[messages.GitStatusBatchResult](), false},
	{"messages.CreateWorkspace", zeroMsg[messages.CreateWorkspace](), false},
	{"messages.DeleteWorkspace", zeroMsg[messages.DeleteWorkspace](), false},
	{"messages.RenameWorkspace", zeroMsg[messages.RenameWorkspace](), false},
	{"messages.AddProject", zeroMsg[messages.AddProject](), false},
	{"messages.RemoveProject", zeroMsg[messages.RemoveProject](), false},
	{"messages.WorkspaceDeleted", zeroMsg[messages.WorkspaceDeleted](), false},
	{"messages.ProjectRemoved", zeroMsg[messages.ProjectRemoved](), false},
	{"messages.WorkspaceDeleteFailed", zeroMsg[messages.WorkspaceDeleteFailed](), false},
	{"messages.ShelveWorkspace", zeroMsg[messages.ShelveWorkspace](), false},
	{"messages.WorkspaceShelved", zeroMsg[messages.WorkspaceShelved](), false},
	{"messages.WorkspaceShelveFailed", zeroMsg[messages.WorkspaceShelveFailed](), false},
	{"messages.RestoreWorkspace", zeroMsg[messages.RestoreWorkspace](), false},
	{"messages.WorkspaceRestored", zeroMsg[messages.WorkspaceRestored](), false},
	{"messages.WorkspaceRestoreFailed", zeroMsg[messages.WorkspaceRestoreFailed](), false},
	{"messages.WorkspaceRestoreSkipped", zeroMsg[messages.WorkspaceRestoreSkipped](), false},
	{"messages.WorkspaceCommitted", zeroMsg[messages.WorkspaceCommitted](), false},
	{"messages.MergeWorkspace", zeroMsg[messages.MergeWorkspace](), false},
	{"messages.MergeWorkspaceRefused", zeroMsg[messages.MergeWorkspaceRefused](), false},
	{"messages.WorkspaceMerged", zeroMsg[messages.WorkspaceMerged](), false},
	{"messages.WorkspaceMergeAborted", zeroMsg[messages.WorkspaceMergeAborted](), false},
	{"messages.ToggleWorkspaceScript", zeroMsg[messages.ToggleWorkspaceScript](), false},
	{"messages.RerunWorkspaceScript", zeroMsg[messages.RerunWorkspaceScript](), false},
	{"messages.WorkspaceScriptStateChanged", zeroMsg[messages.WorkspaceScriptStateChanged](), false},
	{"messages.WorkspaceOnDoneResult", zeroMsg[messages.WorkspaceOnDoneResult](), false},
	{"messages.FileWatcherEvent", zeroMsg[messages.FileWatcherEvent](), true},
	{"messages.StateWatcherEvent", zeroMsg[messages.StateWatcherEvent](), true},

	// updateDialogShowMsg (app_input_dispatch.go)
	{"messages.ShowWelcome", zeroMsg[messages.ShowWelcome](), false},
	{"messages.ShowCommandsPalette", zeroMsg[messages.ShowCommandsPalette](), false},
	{"messages.ToggleKeymapHints", zeroMsg[messages.ToggleKeymapHints](), false},
	{"messages.ShowQuitDialog", zeroMsg[messages.ShowQuitDialog](), false},
	{"messages.ShowAddProjectDialog", zeroMsg[messages.ShowAddProjectDialog](), false},
	{"messages.ShowCreateWorkspaceDialog", zeroMsg[messages.ShowCreateWorkspaceDialog](), false},
	{"messages.ShowDeleteWorkspaceDialog", zeroMsg[messages.ShowDeleteWorkspaceDialog](), false},
	{"messages.ShowRenameWorkspaceDialog", zeroMsg[messages.ShowRenameWorkspaceDialog](), false},
	{"messages.ShowWorkspaceEnvDialog", zeroMsg[messages.ShowWorkspaceEnvDialog](), false},
	{"messages.ShowProjectEnvDialog", zeroMsg[messages.ShowProjectEnvDialog](), false},
	{"messages.ShowShelveWorkspaceDialog", zeroMsg[messages.ShowShelveWorkspaceDialog](), false},
	{"messages.ShowBulkShelveWorkspaceDialog", zeroMsg[messages.ShowBulkShelveWorkspaceDialog](), false},
	{"messages.ShowBulkRestoreWorkspaceDialog", zeroMsg[messages.ShowBulkRestoreWorkspaceDialog](), false},
	{"messages.ShowBulkPurgeWorkspaceDialog", zeroMsg[messages.ShowBulkPurgeWorkspaceDialog](), false},
	{"messages.ShowBulkDeleteWorkspaceDialog", zeroMsg[messages.ShowBulkDeleteWorkspaceDialog](), false},
	{"messages.ShowWorkspaceScriptsDialog", zeroMsg[messages.ShowWorkspaceScriptsDialog](), false},
	{"messages.ShowRunScriptOutput", zeroMsg[messages.ShowRunScriptOutput](), false},
	{"messages.ShowScriptOutput", zeroMsg[messages.ShowScriptOutput](), false},
	{"messages.ShowWorkspaceStatus", zeroMsg[messages.ShowWorkspaceStatus](), false},
	{"app.lifecycleScriptExitedMsg", zeroMsg[lifecycleScriptExitedMsg](), false},
	{"messages.RunScriptStatusResult", zeroMsg[messages.RunScriptStatusResult](), false},
	{"messages.ShowCommitWorkspaceDialog", zeroMsg[messages.ShowCommitWorkspaceDialog](), false},
	{"messages.ShowMergeWorkspaceDialog", zeroMsg[messages.ShowMergeWorkspaceDialog](), false},
	{"messages.ShowTrustScriptsDialog", zeroMsg[messages.ShowTrustScriptsDialog](), false},
	{"messages.ShowRemoveProjectDialog", zeroMsg[messages.ShowRemoveProjectDialog](), false},
	{"messages.ShowSelectAssistantDialog", zeroMsg[messages.ShowSelectAssistantDialog](), false},
	{"messages.ShowLaunchTaskDialog", zeroMsg[messages.ShowLaunchTaskDialog](), false},
	{"messages.ShowSettingsDialog", zeroMsg[messages.ShowSettingsDialog](), false},
	{"messages.ShowCleanupTmuxDialog", zeroMsg[messages.ShowCleanupTmuxDialog](), false},
	{"common.ThemePreview", zeroMsg[common.ThemePreview](), false},
	{"common.SettingsResult", zeroMsg[common.SettingsResult](), false},
	{"common.EnvDialogResult", zeroMsg[common.EnvDialogResult](), false},
	{"common.ScriptsDialogResult", zeroMsg[common.ScriptsDialogResult](), false},
	{"common.OutputDialogResult", zeroMsg[common.OutputDialogResult](), false},
	{"app.runSessionsEnumeratedMsg", zeroMsg[runSessionsEnumeratedMsg](), false},
	{"app.runOutputOpenedMsg", zeroMsg[runOutputOpenedMsg](), false},
	{"app.workspaceStatusReadyMsg", zeroMsg[workspaceStatusReadyMsg](), false},
	{"app.reservationReleaseResultMsg", zeroMsg[reservationReleaseResultMsg](), false},
	{"app.untrustResultMsg", zeroMsg[untrustResultMsg](), false},
	{"app.runAttachTargetMsg", zeroMsg[runAttachTargetMsg](), false},
	{"app.runOutputTickMsg", zeroMsg[runOutputTickMsg](), true},
	{"app.runOutputRefreshedMsg", zeroMsg[runOutputRefreshedMsg](), true},
	{"app.renameWorkspaceResultMsg", zeroMsg[renameWorkspaceResultMsg](), false},
	{"app.workspaceScriptsSavedMsg", zeroMsg[workspaceScriptsSavedMsg](), false},
	{"app.workspaceEnvSavedMsg", zeroMsg[workspaceEnvSavedMsg](), false},
	{"app.projectEnvReadyMsg", zeroMsg[projectEnvReadyMsg](), false},
	{"app.projectEnvSavedMsg", zeroMsg[projectEnvSavedMsg](), false},

	// updateUpgradeMsg (app_input_dispatch.go)
	{"messages.UpdateCheckComplete", zeroMsg[messages.UpdateCheckComplete](), false},
	{"messages.TriggerUpgrade", zeroMsg[messages.TriggerUpgrade](), false},
	{"messages.UpgradeComplete", zeroMsg[messages.UpgradeComplete](), false},

	// handleDialogResultMsg (app_dialog_result_binding.go) — consumed by the
	// pre-switch guard before the main routing table. Both are one-shot
	// results, never periodic chains.
	{"app.boundDialogResultMsg", zeroMsg[boundDialogResultMsg](), false},
	{"common.DialogResult", zeroMsg[common.DialogResult](), false},
	// handleMouseMsg (app_input_mouse.go) re-dispatches the four tea mouse
	// types already classified above — walking it adds no new names.

	// center.PTYRestart reaches Update through the default sink (routed to
	// center.Update, not a named case) — a panic still surfaces the type to
	// rearmCmdForPanic, so it is classified here even though the AST walker
	// cannot see it in a case list.
	{"center.PTYRestart", zeroMsg[center.PTYRestart](), true},
	// center.PTYCursorRefresh travels the same default-sink path. It is
	// deliberately non-rearmed: scheduleChatCursorRefreshLocked's
	// stale-deadline rule re-arms on the next event even when a panic drops
	// the delivered tick. Its model-internal completeness coverage (and the
	// rest of center.Model.Update / sidebar TerminalModel.Update's dispatch
	// surfaces) lives in each model's own rearm completeness test — most of
	// those types are unexported and cannot be named from this package.
	{"center.PTYCursorRefresh", zeroMsg[center.PTYCursorRefresh](), false},
}

// zeroMsg returns a zero-value sample of an exported message type — a var
// declaration, not a composite literal, so types with unexported fields
// (e.g. sidebar.SidebarTabWritten) construct cleanly.
func zeroMsg[T any]() tea.Msg {
	var z T
	return z
}

// TestRearmCmdForPanic_ClassifiesEveryDispatchedMessage is the completeness
// contract for the panic rearm: every message type App.update routes through
// its top-level type switches must appear in rearmChainClassification, so a
// new periodic chain cannot land silently in rearmCmdForPanic's nil default.
// The dispatched set is extracted from the source — adding a case without
// classifying it fails this test with the type names.
func TestRearmCmdForPanic_ClassifiesEveryDispatchedMessage(t *testing.T) {
	dispatched := dispatchedMsgTypeNames(t)

	classified := make(map[string]bool, len(rearmChainClassification))
	for _, row := range rearmChainClassification {
		classified[row.name] = row.chain
	}
	var missing []string
	for name := range dispatched {
		if _, ok := classified[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("dispatched message types missing a rearm classification — add each to rearmChainClassification: %v", missing)
	}

	// Classified-nil rows must produce nil on a bare App: a type wired to a
	// starter later flips this assertion instead of silently arming.
	a := &App{}
	for _, row := range rearmChainClassification {
		if row.chain {
			continue // non-nil proof lives in the chain's own tests
		}
		if cmd := a.rearmCmdForPanic(row.msg); cmd != nil {
			t.Errorf("%s classified non-periodic but rearmCmdForPanic armed a chain", row.name)
		}
	}

	// Classified-vs-mapped cross-check: every chain=true row must be an
	// explicit case in rearmCmdForPanic's switch. The switch's default
	// delegates to center.RearmForPanic — which self-filters to nil for
	// anything not a center chain — so a chain row that drifts out of the
	// case list silently stops rearming (exactly how the sidebar selection
	// tick shipped unrouted). Unexported center chain types can't appear
	// in this registry and are covered by the default + center's own
	// completeness test.
	mapped := rearmCaseTypeNames(t)
	for _, row := range rearmChainClassification {
		if !row.chain {
			continue
		}
		if !mapped[row.name] {
			t.Errorf("%s classified chain=true but has no case in rearmCmdForPanic — it would never rearm", row.name)
		}
	}
}

// rearmCaseTypeNames extracts the message types explicitly cased in
// rearmCmdForPanic's `switch ... := msg.(type)` — the classified-vs-mapped
// half of the completeness check. Model-delegated chains (sidebar, center
// exports) are still explicit cases; only center's unexported types rely
// on the default delegation.
func rearmCaseTypeNames(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "app_rearm.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing app_rearm.go: %v", err)
	}
	names := map[string]bool{}
	found := false
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "rearmCmdForPanic" || fd.Body == nil {
			continue
		}
		found = true
		collectMsgSwitchCases(fd.Body, names)
	}
	if !found {
		t.Fatal("rearmCmdForPanic not found in app_rearm.go")
	}
	if len(names) == 0 {
		t.Fatal("no case types extracted — AST walker drifted from the source")
	}
	return names
}

// dispatchedMsgTypeNames extracts the type names routed by App.update's
// top-level dispatch: update()'s inline switch plus the update*Msg
// sub-dispatchers, the pre-switch dialog-result router, and the mouse
// re-dispatcher — the last two were the blind spot that let a periodic
// chain land in a pre-switch path unclassified. Only
// `switch msg := msg.(type)` statements are walked — nested switches on
// other values (e.g. m.activeTab) are not top-level dispatch.
func dispatchedMsgTypeNames(t *testing.T) map[string]bool {
	t.Helper()
	funcs := map[string][]string{
		"app_input.go":                 {"update"},
		"app_input_dispatch.go":        {"updateUpgradeMsg", "updateTabMsg", "updateTmuxMsg", "updateWorkspaceLifecycleMsg", "updateDialogShowMsg"},
		"app_input_preswitch.go":       {"handlePreSwitchInput"},
		"app_dialog_result_binding.go": {"handleDialogResultMsg"},
		"app_input_mouse.go":           {"handleMouseMsg"},
	}
	names := map[string]bool{}
	for file, wantFuncs := range funcs {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", file, err)
		}
		for _, fn := range wantFuncs {
			found := false
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Name.Name != fn || fd.Body == nil {
					continue
				}
				found = true
				collectMsgSwitchCases(fd.Body, names)
			}
			if !found {
				t.Fatalf("dispatch function %s not found in %s", fn, file)
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("no dispatched message types extracted — AST walker drifted from the source")
	}
	return names
}

// collectMsgSwitchCases walks a function body for `switch msg := msg.(type)`
// statements and records each case's message type under its canonical name:
// package-qualified types as pkg.Type, package-local types as app.Type.
func collectMsgSwitchCases(body *ast.BlockStmt, names map[string]bool) {
	ast.Inspect(body, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSwitchStmt)
		if !ok || !typeSwitchOnMsg(ts) {
			return true
		}
		for _, stmt := range ts.Body.List {
			clause, ok := stmt.(*ast.CaseClause)
			if !ok {
				continue
			}
			for _, expr := range clause.List {
				if name := caseTypeName(expr); name != "" {
					names[name] = true
				}
			}
		}
		return true
	})
}

// typeSwitchOnMsg reports whether a TypeSwitchStmt asserts on the identifier
// `msg` — the dispatch variable every top-level router uses.
func typeSwitchOnMsg(ts *ast.TypeSwitchStmt) bool {
	assign, ok := ts.Assign.(*ast.AssignStmt)
	if !ok || len(assign.Rhs) != 1 {
		return false
	}
	ta, ok := assign.Rhs[0].(*ast.TypeAssertExpr)
	if !ok {
		return false
	}
	ident, ok := ta.X.(*ast.Ident)
	return ok && ident.Name == "msg"
}

// caseTypeName renders a case type expression to its canonical form:
// pkg.Type for selector expressions (the selector's package alias matches
// reflect's package-name form), app.Type for package-local idents.
func caseTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok {
			return x.Name + "." + e.Sel.Name
		}
	case *ast.Ident:
		return "app." + e.Name
	case *ast.StarExpr:
		return "*" + caseTypeName(e.X)
	}
	return ""
}
