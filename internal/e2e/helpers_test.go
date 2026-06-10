package e2e

import (
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
func deleteSelectedWorkspace(t *testing.T, session *PTYSession, timeout time.Duration) {
	t.Helper()
	sendPrefixCommand(t, session, "d")
	waitForUIContains(t, session, "Delete Workspace", timeout)
	if err := session.SendString("h"); err != nil {
		t.Fatalf("select Yes in delete dialog: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := session.SendString("\r"); err != nil {
		t.Fatalf("confirm delete: %v", err)
	}
}
