package center

import (
	"sync"
	"testing"

	appPty "github.com/andyrewlee/amux/internal/pty"
)

// TestSendToTerminal_AgentNilledConcurrently exercises the cross-goroutine race
// between the tab-actor send funnel (sendToTerminal) reading tab.Agent and the
// Update goroutine reassigning/nilling tab.Agent under tab.mu (detach/stop).
//
// Before the snapshot-under-lock fix, sendToTerminal loaded tab.Agent several
// times across its nil-check and dereference with no lock, so an interleaved
// `tab.Agent = nil` could nil-panic the actor goroutine; -race also reports the
// unsynchronized access. After the fix it snapshots the agent once under
// tab.mu, so this test runs clean.
func TestSendToTerminal_AgentNilledConcurrently(t *testing.T) {
	dir := t.TempDir()
	term, err := appPty.NewWithSize("cat >/dev/null", dir, nil, 24, 80)
	if err != nil {
		t.Fatalf("expected test PTY terminal: %v", err)
	}
	defer func() { _ = term.Close() }()

	m := newTestModel()
	ws := newTestWorkspace("ws", dir)
	tabID := TabID("tab-agent-race")
	workspaceID := string(ws.ID())

	agent := &appPty.Agent{Terminal: term}
	tab := &Tab{
		ID:        tabID,
		Assistant: "codex",
		Workspace: ws,
		Agent:     agent,
	}

	const iterations = 5000
	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: the tab-actor send funnel.
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			m.sendToTerminal(tab, "x", tabID, workspaceID, "Input")
		}
	}()

	// Goroutine 2: the Update goroutine nilling/restoring tab.Agent under lock,
	// as detach/stop/reattach do.
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			tab.mu.Lock()
			tab.Agent = nil
			tab.mu.Unlock()
			tab.mu.Lock()
			tab.Agent = agent
			tab.mu.Unlock()
		}
	}()

	wg.Wait()
}
