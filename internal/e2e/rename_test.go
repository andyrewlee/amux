package e2e

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// The rename flow is key-driven end to end: dashboard 'R' opens a prefilled
// input dialog, Enter persists through the store, and the sidebar/dashboard
// labels + toast must reflect the new name without a restart.
func TestWorkspaceRenameThroughDialog(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)
	writeConfig(t, home, false)
	binDir := writeStubAssistant(t, home, "claude")
	server := fmt.Sprintf("amux-e2e-rename-%d", time.Now().UnixNano())
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
	createWorkspaceWithAgent(t, session, "oldname", tmux.Options{ServerName: server, ConfigPath: "/dev/null"}, workspaceAgentTimeout)

	// R opens the rename dialog only on a dashboard workspace row, and only when
	// the dashboard has focus. Clicking the row selects it but also hands focus
	// to the center pane, so focus the dashboard afterward (same order as the
	// delete flow: select, then focus left).
	selectWorkspaceRow(t, session, "oldname", workspaceAgentTimeout)
	sendPrefixCommand(t, session, "h")
	if err := session.SendString("R"); err != nil {
		t.Fatalf("open rename dialog: %v", err)
	}
	waitForUIContains(t, session, "Rename Workspace", workspaceAgentTimeout)

	// The dialog prefills the current name; clear it with backspaces before
	// typing the replacement (extra backspaces on an empty input are no-ops).
	if err := session.SendString(strings.Repeat("\x7f", len("oldname")+8)); err != nil {
		t.Fatalf("clear prefilled name: %v", err)
	}
	if err := session.SendString("renamed-ws\r"); err != nil {
		t.Fatalf("submit new name: %v", err)
	}

	waitForUIContains(t, session, "renamed-ws", workspaceAgentTimeout)
	waitForUIContains(t, session, "Renamed workspace to renamed-ws", workspaceAgentTimeout)
	if err := session.WaitForAbsent("oldname", workspaceAgentTimeout); err != nil {
		t.Fatalf("old workspace name still rendered after rename\n\nScreen:\n%s", session.ScreenASCII())
	}

	// The rename is persisted — a clean restart must render the new name.
	quitApp(t, session)
	if err := session.WaitForExit(persistenceTimeout); err != nil {
		t.Fatalf("waiting for exit: %v", err)
	}

	restart, restartCleanup, err := StartPTYSession(PTYOptions{
		Home: home,
		Env:  sessionEnv(binDir, server),
	})
	if err != nil {
		t.Fatalf("restart session: %v", err)
	}
	defer restartCleanup()
	waitForUIContains(t, restart, "renamed-ws", workspaceAgentTimeout)
}
