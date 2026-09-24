package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
)

// dialogResultHandler acts on a confirmed result for one App-owned dialog.
// dlg is the dialogContext captured when the dialog was opened (project,
// workspace, trustScriptsHash, mergeBase) — the same values the inline switch
// cases used to read off a.dlg.
type dialogResultHandler func(a *App, result common.DialogResult, dlg dialogContext) tea.Cmd

// appDialogHandlers is the single registration point for dialogs the App
// itself owns (as opposed to dialogs owned by center/sidebar components).
// HOW TO ADD AN APP-LEVEL DIALOG: (1) add a Dialog* constant in app_core.go,
// (2) add ONE entry here mapping it to a handler func (defined next to the
// other dialogResult* funcs in app_input_dialogs.go), (3) write the show func
// that builds the widget and calls presentDialog. Routing, the ID allow-list,
// and result dispatch all derive from this table — there is no second place
// to update. TestAppDialogIDsCoverConstants asserts every Dialog* constant is
// registered.
var appDialogHandlers = map[string]dialogResultHandler{
	DialogAddProject:           dialogResultAddProject,
	DialogCreateWorkspace:      dialogResultCreateWorkspace,
	DialogDeleteWorkspace:      dialogResultDeleteWorkspace,
	DialogRenameWorkspace:      dialogResultRenameWorkspace,
	DialogCommitWorkspace:      dialogResultCommitWorkspace,
	DialogMergeWorkspace:       dialogResultMergeWorkspace,
	DialogMergeConflict:        dialogResultMergeConflict,
	DialogTrustScripts:         dialogResultTrustScripts,
	DialogShelveWorkspace:      dialogResultShelveWorkspace,
	DialogBulkShelveWorkspace:  dialogResultBulkShelveWorkspace,
	DialogBulkRestoreWorkspace: dialogResultBulkRestoreWorkspace,
	DialogBulkPurgeWorkspace:   dialogResultBulkPurgeWorkspace,
	DialogRemoveProject:        dialogResultRemoveProject,
	// AgentPickerDialogID is the runtime ID emitted by common.NewAgentPicker;
	// the App owns assistant selection, so it must route here, not a component.
	common.AgentPickerDialogID: dialogResultAgentPicker,
	DialogQuit:                 dialogResultQuit,
	DialogCleanupTmux:          dialogResultCleanupTmux,
	DialogSaveTranscript:       dialogResultSaveTranscript,
}

// appDialogIDList is the registered-ID list in deterministic-enough form for
// tests; derived from the handler map so the enumeration cannot drift.
var appDialogIDList = func() []string {
	ids := make([]string, 0, len(appDialogHandlers))
	for id := range appDialogHandlers {
		ids = append(ids, id)
	}
	return ids
}()

// appDialogIDs is the set form of appDialogIDList, built once at init. Routing
// (handleDialogResultMsg) tests membership against this set instead of an inline
// case list so a newly added dialog cannot silently misroute to a component.
var appDialogIDs = func() map[string]struct{} {
	set := make(map[string]struct{}, len(appDialogIDList))
	for _, id := range appDialogIDList {
		set[id] = struct{}{}
	}
	return set
}()

// isAppDialogID reports whether id names a dialog handled at the App level.
func isAppDialogID(id string) bool {
	_, ok := appDialogIDs[id]
	return ok
}
