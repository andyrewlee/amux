package center

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
)

// ----- SendToTerminal -----

func TestSendToTerminal_EmptyListIsNoOp(t *testing.T) {
	m, _, _ := newActionsModel(t)
	m.SendToTerminal("hello") // must not panic with no tabs
}

func TestSendToTerminal_OutOfRangeActiveIsNoOp(t *testing.T) {
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := chatTab(ws, "tab-0")
	m, _, wsID := newActionsModel(t, tab)
	m.tabs.ActiveByWorkspace[wsID] = 9

	m.SendToTerminal("hello") // out-of-range active index, no panic
}

func TestSendToTerminal_ClosedTabIsNoOp(t *testing.T) {
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := chatTab(ws, "tab-0")
	tab.markClosed()
	m, _, _ := newActionsModel(t, tab)

	m.SendToTerminal("hello") // closed tab, no panic
}

func TestSendToTerminal_NilAgentIsNoOp(t *testing.T) {
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := chatTab(ws, "tab-0") // Agent stays nil
	m, _, _ := newActionsModel(t, tab)

	m.SendToTerminal("hello")
	if tab.Detached {
		t.Fatalf("nil agent should not mark the tab detached")
	}
}

func TestSendToTerminal_WritesToLiveTerminal(t *testing.T) {
	dir := t.TempDir()
	term, err := appPty.NewWithSize("cat >/dev/null", dir, nil, 24, 80)
	if err != nil {
		t.Fatalf("expected test PTY terminal: %v", err)
	}
	defer func() { _ = term.Close() }()

	ws := newTestWorkspace("ws", dir)
	tab := chatTab(ws, "tab-0")
	tab.Agent = &appPty.Agent{Terminal: term}
	m, _, _ := newActionsModel(t, tab)

	m.SendToTerminal("hello")
	if tab.Detached {
		t.Fatalf("successful send should not detach the tab")
	}
}

func TestSendToTerminal_FailureMarksDetached(t *testing.T) {
	dir := t.TempDir()
	term, err := appPty.NewWithSize("cat >/dev/null", dir, nil, 24, 80)
	if err != nil {
		t.Fatalf("expected test PTY terminal: %v", err)
	}
	// Close the terminal so SendString fails with io.ErrClosedPipe, exercising
	// the detach-on-error branch.
	_ = term.Close()

	ws := newTestWorkspace("ws", dir)
	tab := chatTab(ws, "tab-0")
	tab.Agent = &appPty.Agent{Terminal: term}
	m, _, _ := newActionsModel(t, tab)

	m.SendToTerminal("hello")

	tab.mu.Lock()
	detached := tab.Detached
	running := tab.Running
	tab.mu.Unlock()
	if !detached {
		t.Fatalf("send failure should mark the tab detached")
	}
	if running {
		t.Fatalf("send failure should clear the running flag")
	}
}

// ----- ScrollActiveTerminalPage -----

func TestScrollActiveTerminalPage_ZeroDirectionIsNoOp(t *testing.T) {
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := chatTab(ws, "tab-0")
	m, _, _ := newActionsModel(t, tab)

	m.ScrollActiveTerminalPage(0) // must not panic and must do nothing
}

func TestScrollActiveTerminalPage_EmptyListIsNoOp(t *testing.T) {
	m, _, _ := newActionsModel(t)
	m.ScrollActiveTerminalPage(1)
}

func TestScrollActiveTerminalPage_OutOfRangeActiveIsNoOp(t *testing.T) {
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := chatTab(ws, "tab-0")
	m, _, wsID := newActionsModel(t, tab)
	m.tabs.ActiveByWorkspace[wsID] = 7

	m.ScrollActiveTerminalPage(1)
}

func TestScrollActiveTerminalPage_NilTerminalIsNoOp(t *testing.T) {
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := chatTab(ws, "tab-0") // no Terminal allocated
	m, _, _ := newActionsModel(t, tab)

	// Active tab has no terminal viewport; scroll should be a safe no-op.
	m.ScrollActiveTerminalPage(1)
	m.ScrollActiveTerminalPage(-1)
}

// ----- GetTabsInfo -----

func TestGetTabsInfo_MapsStatusAndActiveIndex(t *testing.T) {
	ws := newTestWorkspace("ws", "/repo/ws")
	running := &Tab{
		ID: "t-run", Name: "run", Assistant: "claude", Workspace: ws,
		Running: true, SessionName: "amux-run",
	}
	detached := &Tab{
		ID: "t-det", Name: "det", Assistant: "codex", Workspace: ws,
		Running: true, Detached: true, SessionName: "amux-det",
	}
	stopped := &Tab{
		ID: "t-stop", Name: "stop", Assistant: "claude", Workspace: ws,
	}
	m, _, wsID := newActionsModel(t, running, detached, stopped)
	m.tabs.ActiveByWorkspace[wsID] = 2

	infos, active := m.GetTabsInfo()
	if active != 2 {
		t.Fatalf("active index = %d, want 2", active)
	}
	if len(infos) != 3 {
		t.Fatalf("expected 3 tab infos, got %d", len(infos))
	}

	want := []data.TabInfo{
		{Assistant: "claude", Name: "run", SessionName: "amux-run", Status: "running"},
		{Assistant: "codex", Name: "det", SessionName: "amux-det", Status: "detached"},
		{Assistant: "claude", Name: "stop", SessionName: "", Status: "stopped"},
	}
	for i, w := range want {
		got := infos[i]
		if got.Assistant != w.Assistant || got.Name != w.Name ||
			got.SessionName != w.SessionName || got.Status != w.Status {
			t.Fatalf("info[%d] = %+v, want %+v", i, got, w)
		}
	}
}

func TestGetTabsInfo_DetachedTakesPrecedenceOverRunning(t *testing.T) {
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := &Tab{
		ID: "t", Name: "n", Assistant: "claude", Workspace: ws,
		Running: true, Detached: true,
	}
	m, _, _ := newActionsModel(t, tab)

	infos, _ := m.GetTabsInfo()
	if infos[0].Status != "detached" {
		t.Fatalf("detached must win over running, got %q", infos[0].Status)
	}
}

func TestGetTabsInfo_FallsBackToAgentSession(t *testing.T) {
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := &Tab{
		ID: "t", Name: "n", Assistant: "claude", Workspace: ws,
		Running: true,
		Agent:   &appPty.Agent{Session: "agent-session"},
	}
	m, _, _ := newActionsModel(t, tab)

	infos, _ := m.GetTabsInfo()
	if infos[0].SessionName != "agent-session" {
		t.Fatalf("expected session fallback to agent session, got %q", infos[0].SessionName)
	}
}

func TestGetTabsInfo_SkipsNilTabs(t *testing.T) {
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := chatTab(ws, "t")
	m, _, wsID := newActionsModel(t, nil, tab, nil)
	m.tabs.ActiveByWorkspace[wsID] = 1

	infos, active := m.GetTabsInfo()
	if len(infos) != 1 {
		t.Fatalf("expected nil tabs to be skipped, got %d infos", len(infos))
	}
	if active != 1 {
		t.Fatalf("active index should be preserved, got %d", active)
	}
}

func TestGetTabsInfo_EmptyReturnsNilSlice(t *testing.T) {
	m, _, _ := newActionsModel(t)
	infos, active := m.GetTabsInfo()
	if len(infos) != 0 {
		t.Fatalf("expected no tab infos, got %d", len(infos))
	}
	if active != 0 {
		t.Fatalf("expected active index 0, got %d", active)
	}
}
