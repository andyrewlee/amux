package center

import (
	"testing"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
)

// These tests exercise AddTabsFromWorkspace from model_tabs_session.go, which
// merges newly-discovered tabs into an existing workspace without resetting UI
// state. The returned tea.Cmd batches async reattach commands; those commands
// touch tmux only when invoked, so the tests assert on tab construction and on
// whether a (non-)nil command is produced, never invoking the returned cmd.

func TestAddTabsFromWorkspace_NilWorkspaceReturnsNil(t *testing.T) {
	m := newTestModel()
	if cmd := m.AddTabsFromWorkspace(nil, []data.TabInfo{{Assistant: "claude"}}); cmd != nil {
		t.Fatalf("expected nil cmd for nil workspace, got %T", cmd())
	}
}

func TestAddTabsFromWorkspace_EmptyTabsReturnsNil(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")

	if cmd := m.AddTabsFromWorkspace(ws, nil); cmd != nil {
		t.Fatalf("expected nil cmd for nil tabs, got %T", cmd())
	}
	if cmd := m.AddTabsFromWorkspace(ws, []data.TabInfo{}); cmd != nil {
		t.Fatalf("expected nil cmd for empty tabs, got %T", cmd())
	}
}

func TestAddTabsFromWorkspace_NilConfigReturnsNil(t *testing.T) {
	m := newTestModel()
	m.config = nil
	ws := newTestWorkspace("ws", "/repo/ws")

	if cmd := m.AddTabsFromWorkspace(ws, []data.TabInfo{{Assistant: "claude", Status: "detached"}}); cmd != nil {
		t.Fatalf("expected nil cmd for nil config, got %T", cmd())
	}
	if got := len(m.tabs.ByWorkspace[string(ws.ID())]); got != 0 {
		t.Fatalf("expected no tabs added with nil config, got %d", got)
	}
}

func TestAddTabsFromWorkspace_NilAssistantsMapReturnsNil(t *testing.T) {
	m := newTestModel()
	m.config = &config.Config{Assistants: nil}
	ws := newTestWorkspace("ws", "/repo/ws")

	if cmd := m.AddTabsFromWorkspace(ws, []data.TabInfo{{Assistant: "claude", Status: "detached"}}); cmd != nil {
		t.Fatalf("expected nil cmd when Assistants map is nil, got %T", cmd())
	}
}

func TestAddTabsFromWorkspace_SkipsFilteredTabs(t *testing.T) {
	tests := []struct {
		name string
		info data.TabInfo
	}{
		{name: "empty assistant", info: data.TabInfo{Assistant: "", Status: "detached", SessionName: "s1"}},
		{name: "unknown assistant", info: data.TabInfo{Assistant: "ghostwriter", Status: "detached", SessionName: "s2"}},
		{name: "stopped status", info: data.TabInfo{Assistant: "claude", Status: "stopped", SessionName: "s3"}},
		{name: "stopped status mixed case", info: data.TabInfo{Assistant: "claude", Status: "Stopped", SessionName: "s4"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel()
			ws := newTestWorkspace("ws", "/repo/ws")
			wsID := string(ws.ID())

			cmd := m.AddTabsFromWorkspace(ws, []data.TabInfo{tc.info})
			if cmd != nil {
				t.Fatalf("expected nil cmd when the only tab is filtered, got %T", cmd())
			}
			if got := len(m.tabs.ByWorkspace[wsID]); got != 0 {
				t.Fatalf("expected no tabs added, got %d", got)
			}
		})
	}
}

func TestAddTabsFromWorkspace_AddsDetachedTabWithoutCommand(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	cmd := m.AddTabsFromWorkspace(ws, []data.TabInfo{
		{Assistant: "claude", Name: "Claude", Status: "detached", SessionName: "sess-detached"},
	})
	// A detached tab is materialized synchronously and needs no reattach cmd.
	if cmd != nil {
		t.Fatalf("expected nil cmd for a detached-only batch, got %T", cmd())
	}

	tabs := m.tabs.ByWorkspace[wsID]
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tabs))
	}
	tab := tabs[0]
	if !tab.Detached {
		t.Fatal("expected the added tab to be detached")
	}
	if tab.Running {
		t.Fatal("expected the detached tab not to be running")
	}
	if tab.SessionName != "sess-detached" {
		t.Fatalf("session name = %q, want sess-detached", tab.SessionName)
	}
	if tab.reattachInFlight {
		t.Fatal("expected detached tab not to be flagged reattachInFlight")
	}
}

func TestAddTabsFromWorkspace_AddsRunningPlaceholderWithCommand(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	cmd := m.AddTabsFromWorkspace(ws, []data.TabInfo{
		{Assistant: "claude", Name: "Claude", Status: "running", SessionName: "sess-running"},
	})
	// A running tab is added as a placeholder and queued for async reattach.
	if cmd == nil {
		t.Fatal("expected a reattach cmd for a running tab")
	}

	tabs := m.tabs.ByWorkspace[wsID]
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tabs))
	}
	tab := tabs[0]
	if !tab.Detached {
		t.Fatal("expected the placeholder to start detached before reattach")
	}
	if !tab.reattachInFlight {
		t.Fatal("expected the running placeholder to be flagged reattachInFlight")
	}
	if tab.SessionName != "sess-running" {
		t.Fatalf("session name = %q, want sess-running", tab.SessionName)
	}
}

func TestAddTabsFromWorkspace_SkipsSessionAlreadyOpen(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	// An existing live tab already owns "sess-1".
	m.tabs.ByWorkspace[wsID] = []*Tab{
		{ID: TabID("existing"), Assistant: "claude", Workspace: ws, SessionName: "sess-1", Running: true},
	}

	cmd := m.AddTabsFromWorkspace(ws, []data.TabInfo{
		{Assistant: "claude", Status: "detached", SessionName: "sess-1"},
	})
	if cmd != nil {
		t.Fatalf("expected nil cmd when the session is already open, got %T", cmd())
	}
	if got := len(m.tabs.ByWorkspace[wsID]); got != 1 {
		t.Fatalf("expected the duplicate session not to add a tab, got %d tabs", got)
	}
}

func TestAddTabsFromWorkspace_DedupesSessionWithinBatch(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	cmd := m.AddTabsFromWorkspace(ws, []data.TabInfo{
		{Assistant: "claude", Status: "detached", SessionName: "dup"},
		{Assistant: "claude", Status: "detached", SessionName: "dup"},
	})
	if cmd != nil {
		t.Fatalf("expected nil cmd for detached-only batch, got %T", cmd())
	}
	if got := len(m.tabs.ByWorkspace[wsID]); got != 1 {
		t.Fatalf("expected duplicate session within batch to be added once, got %d tabs", got)
	}
}

func TestAddTabsFromWorkspace_MatchesExistingTabAgentSession(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	// Existing tab carries its session on the Agent rather than SessionName, so
	// dedupe must fall back to the agent's Session field.
	agentTab := &Tab{
		ID:        TabID("existing"),
		Assistant: "claude",
		Workspace: ws,
		Running:   true,
		Agent:     &appPty.Agent{Workspace: ws, Session: "agent-sess"},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{agentTab}

	cmd := m.AddTabsFromWorkspace(ws, []data.TabInfo{
		{Assistant: "claude", Status: "detached", SessionName: "agent-sess"},
	})
	if cmd != nil {
		t.Fatalf("expected nil cmd when session matches an existing tab's agent session, got %T", cmd())
	}
	if got := len(m.tabs.ByWorkspace[wsID]); got != 1 {
		t.Fatalf("expected no new tab when agent session matches, got %d tabs", got)
	}
}

func TestAddTabsFromWorkspace_EmptySessionAddsEachTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	// Tabs with no session name are never deduped against one another.
	cmd := m.AddTabsFromWorkspace(ws, []data.TabInfo{
		{Assistant: "claude", Status: "detached"},
		{Assistant: "claude", Status: "detached"},
	})
	if cmd != nil {
		t.Fatalf("expected nil cmd for detached-only batch, got %T", cmd())
	}
	if got := len(m.tabs.ByWorkspace[wsID]); got != 2 {
		t.Fatalf("expected both session-less detached tabs to be added, got %d", got)
	}
}

func TestAddTabsFromWorkspace_MixedBatchAddsAllAndBatchesRunning(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	cmd := m.AddTabsFromWorkspace(ws, []data.TabInfo{
		{Assistant: "claude", Status: "detached", SessionName: "d1"},
		{Assistant: "claude", Status: "running", SessionName: "r1"},
		{Assistant: "codex", Status: "running", SessionName: "r2"},
		{Assistant: "unknown", Status: "running", SessionName: "x1"}, // filtered
		{Assistant: "claude", Status: "stopped", SessionName: "s1"},  // filtered
	})
	if cmd == nil {
		t.Fatal("expected a batched cmd because the batch contains running tabs")
	}

	tabs := m.tabs.ByWorkspace[wsID]
	if len(tabs) != 3 {
		t.Fatalf("expected 3 tabs (1 detached + 2 running), got %d", len(tabs))
	}

	bySession := make(map[string]*Tab, len(tabs))
	for _, tab := range tabs {
		bySession[tab.SessionName] = tab
	}
	if d1, ok := bySession["d1"]; !ok || !d1.Detached || d1.reattachInFlight {
		t.Fatalf("d1 should be a plain detached tab, got %+v", d1)
	}
	for _, sess := range []string{"r1", "r2"} {
		tab, ok := bySession[sess]
		if !ok {
			t.Fatalf("expected running session %q to produce a placeholder tab", sess)
		}
		if !tab.reattachInFlight {
			t.Fatalf("expected running placeholder %q to be reattachInFlight", sess)
		}
	}
	if _, ok := bySession["x1"]; ok {
		t.Fatal("expected unknown-assistant tab to be filtered out")
	}
	if _, ok := bySession["s1"]; ok {
		t.Fatal("expected stopped tab to be filtered out")
	}
}
