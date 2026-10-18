package e2e

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// TestEventLogRecallReachable drives the C-Space e binding over a real PTY
// into the Notifications viewer. The empty-ring path toasts first ("No
// notifications yet this session"), which lands the ring its first entry —
// so a second C-Space e opens the recall viewer containing that row. This
// is the reachability complement to the unit tests: the binding, the toast
// capture, and the overlay all fire against the real binary.
func TestEventLogRecallReachable(t *testing.T) {
	skipIfNoGit(t)
	requireRealTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)

	server := fmt.Sprintf("amux-e2e-%d", time.Now().UnixNano())
	defer killTmuxServer(t, server)

	session, cleanup, err := StartPTYSession(PTYOptions{
		Home:   home,
		Env:    sessionEnv(home, server),
		Width:  160,
		Height: 40,
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer cleanup()

	waitForUIContains(t, session, filepath.Base(repo), closeLoopTimeout)

	// First C-Space e on an empty ring produces the "nothing yet" toast —
	// which itself becomes ring entry #1.
	sendPrefixCommand(t, session, "e")
	waitForUIContains(t, session, "No notifications yet this session", persistenceTimeout)

	// Second C-Space e now opens the viewer, containing the toast the first
	// press just recorded. "Notifications" is the dialog's title — it only
	// renders from the recall overlay, never the transient toast.
	sendPrefixCommand(t, session, "e")
	waitForUIContains(t, session, "Notifications", persistenceTimeout)
	waitForUIContains(t, session, "No notifications yet this session", persistenceTimeout)
}
