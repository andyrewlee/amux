package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/ui/common"
)

// TestAppDialogIDsCoverConstants guards the single-source-of-truth contract:
// every Dialog* constant declared in app_core.go (plus the agent-picker runtime
// ID) must be a member of appDialogIDs. If a new dialog constant is added
// without registering it here, its DialogResult would silently misroute to a
// component instead of handleDialogResult — this test fails first.
func TestAppDialogIDsCoverConstants(t *testing.T) {
	constants := []string{
		DialogAddProject,
		DialogCreateWorkspace,
		DialogDeleteWorkspace,
		DialogTrustScripts,
		DialogRemoveProject,
		DialogSelectAssistant,
		common.AgentPickerDialogID,
		DialogQuit,
		DialogCleanupTmux,
	}
	for _, id := range constants {
		if !isAppDialogID(id) {
			t.Errorf("dialog ID %q is not registered in appDialogIDs; "+
				"its DialogResult would misroute to a component", id)
		}
	}
}

// TestAppDialogIDsNoDuplicates ensures the list form has no duplicate entries
// (which would mask a missing distinct ID and inflate the set's apparent size).
func TestAppDialogIDsNoDuplicates(t *testing.T) {
	seen := make(map[string]struct{}, len(appDialogIDList))
	for _, id := range appDialogIDList {
		if _, dup := seen[id]; dup {
			t.Errorf("duplicate dialog ID %q in appDialogIDList", id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != len(appDialogIDs) {
		t.Errorf("appDialogIDList has %d unique IDs but appDialogIDs has %d",
			len(seen), len(appDialogIDs))
	}
}

// TestUnknownDialogIDIsNotAppLevel sanity-checks the negative case: an ID that
// is not in the registry must route away from the App.
func TestUnknownDialogIDIsNotAppLevel(t *testing.T) {
	if isAppDialogID("definitely-not-a-real-dialog") {
		t.Fatal("unexpected: unknown ID reported as an App-level dialog")
	}
}
