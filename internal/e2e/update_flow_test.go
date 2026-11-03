package e2e

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The e2e binary builds as version "dev", and Updater.Check skips dev builds
// entirely (internal/update/updater.go) — the observable contract is that no
// update affordance ever surfaces, including in Settings, and no network call
// stalls startup. One exception ships from the release flow itself: when HEAD
// is exactly at a release tag, go1.26 stamps the module version into a plain
// `go build`'s build info (internal/e2e builds without ldflags), so the
// pre-push hook's e2e rerun — which runs after `make release` creates the
// tag — drives a stamped, non-dev binary. Both shapes must hold the same
// contract: the settings Version row reads "Development build" or the
// stamped version, and no update affordance ever surfaces. A positive
// update-available path needs a release check URL seam, which does not
// exist; injecting one would be a product decision, not test scaffolding
// (the check→settings→upgrade chain is unit-covered at
// internal/app/app_input_dialogs_more_test.go).
func TestDevBuildShowsNoUpdatePrompt(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	// The Settings Version row differs by build shape; everything else the
	// test asserts is shape-independent.
	versionLabel := "Development build"
	if v := e2eBinaryVersion(t); v != "dev" && v != "" {
		versionLabel = v
	}

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
	// No update toast/dialog may surface at startup — on a dev build because
	// the updater skips dev versions entirely, on a tagged build because the
	// stamp is the newest release. A failed check (offline box) surfaces
	// nothing either: the error is logged, not toasted.
	assertScreenNeverContains(t, session, []string{"[Update to", "Upgraded to"}, 1500*time.Millisecond)

	sendPrefixCommand(t, session, "S")
	// "Settings" alone is ambiguous — the sidebar footer already shows a
	// "[Settings]" hint. "restart to apply" only renders inside the dialog.
	waitForUIContains(t, session, "restart to apply", workspaceAgentTimeout)
	// The Version row sits at the bottom of the scrollable dialog. Tab advances
	// one settings item at a time (theme → three tmux fields → three interface
	// fields → assistants → close; update is skipped when unavailable — true
	// in both shapes, since a tagged build stamps the newest release); exactly
	// eight tabs land focus on [Close], which pulls the scroll offset to the
	// bottom. Extra tabs wrap, so the count matters.
	if err := session.SendString(strings.Repeat("\t", 8)); err != nil {
		t.Fatalf("scroll settings to bottom: %v", err)
	}
	waitForUIContains(t, session, versionLabel, workspaceAgentTimeout)
	waitForUIContains(t, session, "[Close]", workspaceAgentTimeout)
	assertScreenNeverContains(t, session, []string{"[Update to"}, 1500*time.Millisecond)

	if err := session.SendString("\x1b"); err != nil {
		t.Fatalf("close settings: %v", err)
	}
	if err := session.WaitForAbsent(versionLabel, persistenceTimeout); err != nil {
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

// e2eBinaryVersion reports the version the e2e-built binary reports for
// itself ("dev" for a plain build, the tag when HEAD is exactly at one).
func e2eBinaryVersion(t *testing.T) string {
	t.Helper()
	bin, _, err := buildAmuxBinary()
	if err != nil {
		t.Fatalf("build e2e binary: %v", err)
	}
	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("query e2e binary version: %v\n%s", err, out)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}
