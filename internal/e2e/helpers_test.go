package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

func createWorkspaceFromDashboard(t *testing.T, session *PTYSession, name string) {
	t.Helper()
	if err := session.SendString("G"); err != nil {
		t.Fatalf("jump to create row: %v", err)
	}
	if err := session.SendString("\r"); err != nil {
		t.Fatalf("open create dialog: %v", err)
	}
	waitForUIContains(t, session, "Create Workspace", workspaceAgentTimeout)
	if err := session.SendString(name); err != nil {
		t.Fatalf("enter workspace name: %v", err)
	}
	if err := session.SendString("\r"); err != nil {
		t.Fatalf("confirm workspace name: %v", err)
	}
}

func createWorkspaceAndOpenAgentPicker(t *testing.T, session *PTYSession, name string, timeout time.Duration) {
	t.Helper()
	createWorkspaceFromDashboard(t, session, name)
	waitForUIContains(t, session, "New Agent", timeout)
	selectAgentFromPicker(t, session, 0)
	waitForUIContains(t, session, name, timeout)
	selectWorkspaceRow(t, session, name, timeout)
	waitForUIContains(t, session, "[New agent]", timeout)
	clickVisibleLabel(t, session, "[New agent]", "New Agent", timeout)
	waitForUIContains(t, session, "New Agent", timeout)
}

func createSidebarTerminalTab(t *testing.T, session *PTYSession) {
	t.Helper()
	sendPrefixSequence(t, session, "t", "t")
	waitForUIContains(t, session, "Terminal 2", 10*time.Second)
}

func workspaceIDForRepo(repo string) string {
	ws := data.NewWorkspace("ws", "main", "main", repo, repo)
	return string(ws.ID())
}

// deleteSelectedWorkspace opens the delete-workspace dialog for the active
// workspace via the leader sequence and confirms it. The confirm dialog defaults
// to "No" (cursor on index 1), so "h" moves the selection to "Yes" before Enter.
func deleteSelectedWorkspace(t *testing.T, session *PTYSession, workspaceName string, timeout time.Duration) {
	t.Helper()
	selectWorkspaceRow(t, session, workspaceName, timeout)
	sendPrefixCommand(t, session, "h")
	sendPrefixCommand(t, session, "d")
	waitForUIContains(t, session, "Delete workspace '"+workspaceName+"' and its branch?", timeout)
	if err := session.SendString("h"); err != nil {
		t.Fatalf("select Yes in delete dialog: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if err := session.SendString("h"); err != nil {
		t.Fatalf("keep Yes selected in delete dialog: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if err := session.SendString("\r"); err != nil {
		t.Fatalf("confirm delete: %v", err)
	}
}

func selectWorkspaceRow(t *testing.T, session *PTYSession, workspaceName string, timeout time.Duration) {
	t.Helper()
	rowY := workspaceRowY(t, session, workspaceName, timeout)
	candidates := []int{rowY}
	if rowY > 0 {
		candidates = append(candidates, rowY-1)
	}
	for i, candidateY := range candidates {
		if err := session.SendString(dashboardRowLeftClickInput(120, 30, candidateY)); err != nil {
			t.Fatalf("activate workspace row %q: %v", workspaceName, err)
		}
		time.Sleep(200 * time.Millisecond)
		if !stringsContains(session.ScreenASCII(), "Create Workspace") {
			return
		}
		if i == len(candidates)-1 {
			break
		}
		if err := session.SendString("\x1b"); err != nil {
			t.Fatalf("cancel accidental create dialog: %v", err)
		}
		waitForUIConsistentlyAbsent(t, session, "Create Workspace", 2*time.Second, screenPollInterval)
	}
	t.Fatalf("selecting workspace row %q opened create dialog\n\nScreen:\n%s", workspaceName, session.ScreenASCII())
}

func workspaceRowY(t *testing.T, session *PTYSession, workspaceName string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastScreen string
	for time.Now().Before(deadline) {
		lastScreen = session.ScreenASCII()
		for y, line := range strings.Split(lastScreen, "\n") {
			if strings.Contains(line, workspaceName) && !strings.Contains(line, "Delete workspace") {
				return y
			}
		}
		time.Sleep(screenPollInterval)
	}
	t.Fatalf("timeout waiting for workspace row %q\n\nScreen:\n%s", workspaceName, lastScreen)
	return 0
}

func clickVisibleLabel(t *testing.T, session *PTYSession, label, opened string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastScreen string
	for time.Now().Before(deadline) {
		lastScreen = session.ScreenASCII()
		for y, line := range strings.Split(lastScreen, "\n") {
			if x := strings.Index(line, label); x >= 0 {
				candidates := []int{y}
				if y > 0 {
					candidates = append(candidates, y-1)
				}
				for _, candidateY := range candidates {
					if err := session.SendString(leftClickInput(x+1, candidateY)); err != nil {
						t.Fatalf("click label %q: %v", label, err)
					}
					if session.WaitForContains(opened, 2*time.Second) == nil {
						return
					}
				}
			}
		}
		time.Sleep(screenPollInterval)
	}
	t.Fatalf("timeout clicking label %q to open %q\n\nScreen:\n%s", label, opened, lastScreen)
}
