package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// requestOverlayOpen defers an open while any modal overlay is visible and
// drains the queue once it resolves. End to end: the Settings overlay (not a
// common.Dialog — Show* dialog handlers' dialogOpen() early-drop only guards
// the registry dialog slot) holds the modal slot while workspace creation's
// async setup-complete delivers ShowTrustScriptsDialog; the trust prompt must
// queue, render only after Settings closes, and carry the new config's marker.
//
// Ordering is deterministic by construction: the trust message cannot exist
// until async workspace creation finishes (a real git worktree subprocess),
// while the prefix-driven settings open lands within ~50ms of the assistant
// pick.
func TestOverlayQueueDefersAsyncOpenUntilFirstResolves(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	repo := initRepo(t)
	sentinel := filepath.Join(t.TempDir(), "setup-ran")
	writeRepoScriptConfig(t, repo, sentinel, "queuedmarker")

	session, _, cleanup := startTrustSession(t, repo)
	defer cleanup()

	waitForUIContains(t, session, filepath.Base(repo), workspaceAgentTimeout)
	createWorkspaceFromDashboard(t, session, "qtest")
	waitForUIContains(t, session, "New Agent", workspaceAgentTimeout)
	selectAgentFromPicker(t, session, 0)

	// Open Settings before the async create+setup path can deliver the trust
	// prompt — it must win the modal slot, and the trust open must queue.
	sendPrefixCommand(t, session, "S")
	waitForUIContains(t, session, "restart to apply", workspaceAgentTimeout)

	// While Settings holds the slot, the trust prompt's title stays absent over
	// a window comfortably longer than the skipped-setup completion takes.
	waitForUIConsistentlyAbsent(t, session, "Trust Project Scripts", 2*time.Second, screenPollInterval)

	// Closing Settings drains the queue: the deferred trust prompt renders,
	// carrying the new config's marker in its manifest as proof it is the same
	// queued request, not a fresh prompt.
	if err := session.SendString("\x1b"); err != nil {
		t.Fatalf("close settings: %v", err)
	}
	waitForUIContains(t, session, "Trust Project Scripts", workspaceAgentTimeout)
	waitForUIContains(t, session, "queuedmarker", workspaceAgentTimeout)

	// Decline (Enter on the default "No"): untrusted setup must not have run at
	// any point in the sequence.
	if err := session.SendString("\r"); err != nil {
		t.Fatalf("decline trust dialog: %v", err)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("setup sentinel exists — untrusted script executed")
	}
}
