package center

import (
	"sync"
	"testing"

	appPty "github.com/andyrewlee/amux/internal/pty"
)

// TestAdmitTabInput_AgentNilledConcurrently exercises the cross-goroutine race
// between input admission reading tab.Agent and the Update goroutine
// reassigning/nilling tab.Agent under tab.mu (detach/stop). admitTabInput
// reads the agent under the same lock, so the admission sees either the old
// binding (queues onto its writer, which the transition retires) or the
// missing one (no-terminal no-op) — never a torn read.
func TestAdmitTabInput_AgentNilledConcurrently(t *testing.T) {
	dir := t.TempDir()
	term, err := appPty.NewWithSize("cat >/dev/null", dir, nil, 24, 80)
	if err != nil {
		t.Fatalf("expected test PTY terminal: %v", err)
	}
	defer func() { _ = term.Close() }()

	m := newTestModel()
	ws := newTestWorkspace("ws", dir)
	agent := &appPty.Agent{Terminal: term}
	tab := &Tab{
		ID:        TabID("tab-agent-race"),
		Assistant: "codex",
		Workspace: ws,
		Agent:     agent,
	}
	tab.tabInput.gen = 1

	const iterations = 2000
	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: admission from the update-loop producers.
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			m.admitTabInput(tab, "x", "Input", false)
		}
	}()

	// Goroutine 2: the Update goroutine nilling/restoring tab.Agent under
	// lock, as detach/stop/reattach do.
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

	// Reap any writers the admissions created so no goroutine leaks past the
	// test.
	tab.mu.Lock()
	tab.retireTabInputWriterLocked()
	tab.mu.Unlock()
	tab.joinRetiredInputWriters()
}
