package e2e

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
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

func waitForAgentSessions(t *testing.T, opts tmux.Options, timeout time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		sessions, err := tmux.ListSessionsMatchingTags(map[string]string{
			"@amux":      "1",
			"@amux_type": "agent",
		}, opts)
		if err == nil && len(sessions) > 0 {
			return sessions
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for agent sessions")
	return nil
}

func assertAgentSessionsStayLive(t *testing.T, opts tmux.Options, duration time.Duration) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		sessions, err := tmux.ListSessionsMatchingTags(map[string]string{
			"@amux":      "1",
			"@amux_type": "agent",
		}, opts)
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if len(sessions) == 0 {
			t.Fatalf("expected at least one agent session to stay alive")
		}
		live := false
		for _, name := range sessions {
			state, err := tmux.SessionStateFor(name, opts)
			if err != nil {
				continue
			}
			if state.Exists && state.HasLivePane {
				live = true
				break
			}
		}
		if !live {
			t.Fatalf("agent sessions not live: %v", sessions)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func assertScreenNeverContains(t *testing.T, session *PTYSession, needles []string, duration time.Duration) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		screen := session.ScreenASCII()
		for _, needle := range needles {
			if stringsContains(screen, needle) {
				t.Fatalf("unexpected screen text %q\n\nScreen:\n%s", needle, screen)
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
}
