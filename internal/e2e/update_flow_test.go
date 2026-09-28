package e2e

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The e2e binary builds as version "dev", and Updater.Check skips dev builds
// entirely (internal/update/updater.go) — the observable contract is that no
// update affordance ever surfaces, including in Settings, and no network call
// stalls startup. A positive update-available path needs a release check URL
// seam, which does not exist; injecting one would be a product decision, not
// test scaffolding (the check→settings→upgrade chain is unit-covered at
// internal/app/app_input_dialogs_more_test.go).
func TestDevBuildShowsNoUpdatePrompt(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)
	writeConfig(t, home, false)
	binDir := writeStubAssistant(t, home, "claude")
	server := fmt.Sprintf("amux-e2e-update-%d", time.Now().UnixNano())
	defer killTmuxServer(t, server)

	session, cleanup, err := StartPTYSession(PTYOptions{
		Home: home,
		Env:  sessionEnv(binDir, server),
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer cleanup()

	waitForUIContains(t, session, filepath.Base(repo), workspaceAgentTimeout)
	// No update toast/dialog may surface at startup on a dev build.
	assertScreenNeverContains(t, session, []string{"[Update to", "Upgraded to"}, 1500*time.Millisecond)

	sendPrefixCommand(t, session, "S")
	// "Settings" alone is ambiguous — the sidebar footer already shows a
	// "[Settings]" hint. "restart to apply" only renders inside the dialog.
	waitForUIContains(t, session, "restart to apply", workspaceAgentTimeout)
	// The Version row sits at the bottom of the scrollable dialog. Tab advances
	// one settings item at a time (theme → three tmux fields → assistants →
	// close); exactly five tabs land focus on [Close], which pulls the scroll
	// offset to the bottom. Extra tabs wrap, so the count matters.
	if err := session.SendString(strings.Repeat("\t", 5)); err != nil {
		t.Fatalf("scroll settings to bottom: %v", err)
	}
	waitForUIContains(t, session, "Development build", workspaceAgentTimeout)
	waitForUIContains(t, session, "[Close]", workspaceAgentTimeout)
	assertScreenNeverContains(t, session, []string{"[Update to"}, 1500*time.Millisecond)

	if err := session.SendString("\x1b"); err != nil {
		t.Fatalf("close settings: %v", err)
	}
	if err := session.WaitForAbsent("Development build", persistenceTimeout); err != nil {
		t.Fatalf("settings did not close\n\nScreen:\n%s", session.ScreenASCII())
	}
	// The prefix byte must land after the dialog's dismissal redraw, so wait
	// for the dashboard to be live again before driving the quit flow.
	waitForUIContains(t, session, filepath.Base(repo), persistenceTimeout)
	quitApp(t, session)
	if err := session.WaitForExit(persistenceTimeout); err != nil {
		t.Fatalf("waiting for exit: %v", err)
	}
}
