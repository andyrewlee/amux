package app

import "github.com/andyrewlee/amux/internal/ui/common"

// appDialogIDList is the single source of truth for the dialog IDs that the App
// itself owns (as opposed to dialogs owned by center/sidebar components). Adding
// a new App-level dialog means adding its ID here and a behavioral case in
// handleDialogResult; the routing allow-list in handleDialogResultMsg derives
// from this list, so the two enumerations can no longer drift.
//
// Keep this list in sync with the Dialog* constants in app_core.go and the
// switch in handleDialogResult. TestAppDialogIDsCoverConstants asserts every
// Dialog* constant is present here.
var appDialogIDList = []string{
	DialogAddProject,
	DialogCreateWorkspace,
	DialogDeleteWorkspace,
	DialogRenameWorkspace,
	DialogTrustScripts,
	DialogRemoveProject,
	DialogSelectAssistant,
	// AgentPickerDialogID is the runtime ID emitted by common.NewAgentPicker;
	// it shares handleDialogResult's assistant-selection case with
	// DialogSelectAssistant, so it must route to the App, not a component.
	common.AgentPickerDialogID,
	DialogQuit,
	DialogCleanupTmux,
}

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
