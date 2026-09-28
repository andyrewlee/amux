package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// TestShelveRestorePurgeLifecycle drives the full workspace shelf lifecycle
// through the real binary: create → S on the dashboard row → restore via
// Enter → re-shelve → D (purge). It asserts the whole chain each step —
// dashboard row state, tmux session teardown, worktree on disk, and branch
// survival/deletion — which is the regression net for the lifecycle cluster.
//
// Cursor mechanics worth knowing before editing:
//   - createWorkspaceWithAgent leaves the dashboard cursor on the new
//     workspace row; the workspace occupies row index 3
//     (Home, spacer, Project, Workspace, [Shelved], Create, spacer).
//   - prefix+h returns focus to the dashboard without moving the cursor.
//   - j/k auto-activate the row under the cursor (focus jumps to center), so
//     row targeting uses the row's stable index, not j-walking.
//   - The confirm dialogs default to "No" (cursor 1); "h" picks "Yes", Enter
//     confirms.
func TestShelveRestorePurgeLifecycle(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)
	writeConfig(t, home, false)
	binDir := writeStubAssistant(t, home, "claude")
	server := fmt.Sprintf("amux-e2e-shelve-%d", time.Now().UnixNano())
	opts := tmux.Options{ServerName: server, ConfigPath: "/dev/null"}
	defer killTmuxServer(t, server)

	env := sessionEnv(binDir, server)
	session, cleanup, err := StartPTYSession(PTYOptions{
		Home: home,
		Env:  env,
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer cleanup()

	const wsName = "shelveme"
	wsRoot := filepath.Join(home, ".amux", "workspaces", filepath.Base(repo), wsName)

	waitForUIContains(t, session, filepath.Base(repo), workspaceAgentTimeout)
	createWorkspaceWithAgent(t, session, wsName, opts, workspaceAgentTimeout)

	// Sanity: worktree + agent sessions exist pre-shelve.
	waitForCond(t, func() bool {
		_, err := os.Stat(wsRoot)
		return err == nil
	}, workspaceAgentTimeout, "worktree %s to exist", wsRoot)

	// Step 1: shelve. Cursor is on the new workspace row; prefix+h focuses the
	// dashboard (cursor stays), then S opens the shelve confirm.
	sendPrefixCommand(t, session, "h")
	if err := session.SendString("S"); err != nil {
		t.Fatalf("open shelve dialog: %v", err)
	}
	waitForUIContains(t, session, "Shelve Workspace", persistenceTimeout)
	confirmDialog(t, session, "Shelve Workspace")

	// Assert the whole shelve chain: row tagged, sessions gone, worktree gone,
	// branch kept.
	waitForUIContains(t, session, "shelved", workspaceAgentTimeout)
	waitForCond(t, func() bool {
		_, err := os.Stat(wsRoot)
		return os.IsNotExist(err)
	}, workspaceAgentTimeout, "worktree %s to be removed", wsRoot)
	waitForCond(t, func() bool {
		return !hasSessionsWithPrefix(t, opts, "amux-", 1)
	}, workspaceAgentTimeout, "all amux tmux sessions to be gone after shelve")
	if branches := gitBranchList(t, repo, wsName); branches == "" {
		t.Fatalf("shelve must keep the branch %q", wsName)
	}
	// Two records: the shelved workspace plus the primary checkout's own
	// metadata (the repo registers as a workspace on discovery).
	metadataDirs := globMetadataDirs(t, home)
	if len(metadataDirs) != 2 {
		t.Fatalf("shelve must keep the shelved record + primary checkout record, got %v", metadataDirs)
	}

	// Step 2: restore. The shelve result re-binds the active workspace to the
	// primary checkout, which moves focus to the center pane — return focus to
	// the dashboard first. The cursor stayed on the row index, which is now
	// the shelved row — Enter restores in place (no confirm).
	//
	// The "shelved" tag renders when the store flags land, a beat before the
	// lifecycle in-flight guard releases, so an Enter inside that window is
	// correctly rejected (restoring mid-worktree-removal would race the
	// teardown). A user would just press Enter again — retry until the
	// worktree reappears, bounded by the deadline.
	sendPrefixCommand(t, session, "h")
	deadline := time.Now().Add(workspaceAgentTimeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(wsRoot); err == nil {
			break
		}
		if err := session.SendString("\r"); err != nil {
			t.Fatalf("restore shelved workspace: %v", err)
		}
		time.Sleep(screenPollInterval)
	}
	if _, err := os.Stat(wsRoot); err != nil {
		t.Fatalf("worktree %s not recreated on restore\n\nScreen:\n%s\n\nLog:\n%s", wsRoot, session.ScreenASCII(), readLogTail(t, home))
	}
	waitForUIConsistentlyAbsent(t, session, "shelved", persistenceTimeout, screenPollInterval)
	if branches := gitBranchList(t, repo, wsName); branches == "" {
		t.Fatalf("restore must keep the branch %q", wsName)
	}

	// Step 3: re-shelve, then purge. Restore may have moved focus to the
	// center pane; return to the dashboard, re-shelve, then D on the shelved
	// row (the purge path — same delete dialog, tolerates the absent
	// worktree). The prefix arm is sent by hand (not sendPrefixCommand) so a
	// failure can include the app log — this is where lifecycle error
	// overlays used to swallow the prefix byte.
	if err := session.SendBytes([]byte{0}); err != nil {
		t.Fatalf("send prefix: %v", err)
	}
	if err := session.WaitForContains("Esc cancel", prefixArmTimeout); err != nil {
		t.Fatalf("waiting for prefix palette: %v\n\nLog:\n%s", err, readLogTail(t, home))
	}
	if err := session.SendString("h"); err != nil {
		t.Fatalf("send command: %v", err)
	}
	if err := session.SendString("S"); err != nil {
		t.Fatalf("re-open shelve dialog: %v", err)
	}
	waitForUIContains(t, session, "Shelve Workspace", persistenceTimeout)
	confirmDialog(t, session, "Shelve Workspace")
	waitForUIContains(t, session, "shelved", workspaceAgentTimeout)

	// The second shelve result re-bound focus to center again — refocus the
	// dashboard so D lands on the shelved row under the cursor. The same
	// in-flight window applies to purge: a confirmed delete while the shelve
	// guard still holds is correctly rejected — retry the whole D+confirm
	// sequence until the row is gone, bounded by the deadline.
	sendPrefixCommand(t, session, "h")
	purgeDeadline := time.Now().Add(workspaceAgentTimeout)
	dialogText := "Delete workspace '" + wsName + "' and its branch?"
	for time.Now().Before(purgeDeadline) {
		if !stringsContains(session.ScreenASCII(), "shelved") {
			break
		}
		if err := session.SendString("D"); err != nil {
			t.Fatalf("open purge dialog: %v", err)
		}
		if err := session.WaitForContains(dialogText, 2*time.Second); err == nil {
			confirmDialog(t, session, dialogText)
		} else {
			// D may have landed on the wrong row (e.g. an unexpected dialog is
			// still open) — dismiss whatever is up before retrying.
			_ = session.SendString("\x1b")
		}
		time.Sleep(screenPollInterval)
	}

	// Purge removes the record (row disappears) and the branch.
	waitForUIConsistentlyAbsent(t, session, "shelved", workspaceAgentTimeout, screenPollInterval)
	waitForCond(t, func() bool {
		return gitBranchList(t, repo, wsName) == ""
	}, workspaceAgentTimeout, "branch %q to be deleted on purge", wsName)
	waitForCond(t, func() bool {
		return len(globMetadataDirs(t, home)) == 1
	}, workspaceAgentTimeout, "shelved metadata record removed on purge (primary checkout's remains)")
}

// confirmDialog selects "Yes" in a confirm dialog (cursor defaults to "No")
// and presses Enter. Option selection is styling-only (invisible to
// ScreenASCII), so there is nothing to poll between the keys — instead the
// gesture is atomic: "h" pins the cursor to option 0 absolutely, and h+Enter
// go out in one write so both bytes land on whichever view consumed the
// first. Callers must have already observed dialogTitle on screen — a
// rendered dialog is the input consumer, so the gesture lands. The absent
// wait then confirms dismissal (and fails fast rather than burning the next
// post-state timeout when the gesture was dropped).
func confirmDialog(t *testing.T, session *PTYSession, dialogTitle string) {
	t.Helper()
	if err := session.SendString("h\r"); err != nil {
		t.Fatalf("confirm dialog: %v", err)
	}
	if err := session.WaitForAbsent(dialogTitle, dialogGestureTimeout); err != nil {
		t.Fatalf("dialog %q still on screen after %s\n\nScreen:\n%s", dialogTitle, dialogGestureTimeout, session.ScreenASCII())
	}
}

func gitBranchList(t *testing.T, repo, pattern string) string {
	t.Helper()
	cmd := exec.Command("git", "branch", "--list", pattern)
	cmd.Dir = repo
	cmd.Env = stripGitEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git branch --list %q: %v\n%s", pattern, err, out)
	}
	return string(out)
}

func readLogTail(t *testing.T, home string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(home, ".amux", "logs", "amux-*.log"))
	if err != nil || len(matches) == 0 {
		return "(no log file)"
	}
	data, err := os.ReadFile(matches[len(matches)-1])
	if err != nil {
		return "(unreadable log: " + err.Error() + ")"
	}
	text := string(data)
	if len(text) > 4000 {
		text = text[len(text)-4000:]
	}
	return text
}

func globMetadataDirs(t *testing.T, home string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(home, ".amux", "workspaces-metadata"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read metadata root: %v", err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	return dirs
}
