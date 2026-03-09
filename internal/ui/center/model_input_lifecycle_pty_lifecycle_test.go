package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/vterm"
)

func TestUpdatePtyTabReattachResult_ResetsActivityANSIState(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                TabID("tab-1"),
		Assistant:         "codex",
		Workspace:         ws,
		activityANSIState: ansiActivityString,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_, _ = m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Agent:       &appPty.Agent{Session: "sess-reattach"},
		Rows:        24,
		Cols:        80,
	})

	if tab.activityANSIState != ansiActivityText {
		t.Fatalf("expected activityANSIState reset to text on reattach, got %v", tab.activityANSIState)
	}
	tab.mu.Lock()
	bootstrap := tab.bootstrapActivity
	bootstrapAt := tab.bootstrapLastOutputAt
	tab.mu.Unlock()
	if !bootstrap {
		t.Fatal("expected bootstrapActivity=true on reattach")
	}
	if bootstrapAt.IsZero() {
		t.Fatal("expected bootstrapLastOutputAt to be set on reattach")
	}
}

func TestUpdatePtyTabReattachResult_ResetsStableCursor(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                TabID("tab-1"),
		Assistant:         "codex",
		Workspace:         ws,
		Terminal:          vterm.New(80, 24),
		stableCursorSet:   true,
		stableCursorX:     7,
		stableCursorY:     20,
		lastOutputAt:      time.Now(),
		lastUserInputAt:   time.Now(),
		lastPromptInputAt: time.Now(),
		lastVisibleOutput: time.Now(),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_, _ = m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Agent:       &appPty.Agent{Session: "sess-reattach"},
		Rows:        24,
		Cols:        80,
	})

	if tab.stableCursorSet {
		t.Fatal("expected stable cursor to clear on reattach")
	}
	if tab.stableCursorX != 0 || tab.stableCursorY != 0 {
		t.Fatalf("expected cleared stable cursor coordinates, got (%d,%d)", tab.stableCursorX, tab.stableCursorY)
	}
	if !tab.lastUserInputAt.IsZero() {
		t.Fatal("expected recent-input state to clear on reattach")
	}
	if !tab.lastPromptInputAt.IsZero() {
		t.Fatal("expected recent prompt-input state to clear on reattach")
	}
	if !tab.lastVisibleOutput.IsZero() {
		t.Fatal("expected visible-activity state to clear on reattach")
	}
	if !tab.lastOutputAt.IsZero() {
		t.Fatal("expected recent output state to clear on reattach")
	}
}

func TestUpdatePtyTabReattachResult_NormalizesCapturedScrollbackLFForChatTabs(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-reattach-lf"),
		Assistant: "codex",
		Workspace: ws,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_, _ = m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID:       wsID,
		TabID:             tab.ID,
		Agent:             &appPty.Agent{Session: "sess-reattach-lf"},
		Rows:              24,
		Cols:              80,
		ScrollbackCapture: []byte("abc\nx"),
	})

	if tab.Terminal == nil {
		t.Fatal("expected terminal to be created")
	}
	if len(tab.Terminal.Scrollback) < 2 {
		t.Fatalf("expected at least 2 scrollback lines, got %d", len(tab.Terminal.Scrollback))
	}
	if got := tab.Terminal.Scrollback[1][0].Rune; got != 'x' {
		t.Fatalf("expected captured scrollback LF to reset to col 0, got %q", got)
	}
}

func TestHandlePtyTabCreated_NewTabNormalizesCapturedScrollbackLFForChatTabs(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	_ = m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace:         ws,
		Assistant:         "codex",
		Agent:             &appPty.Agent{Session: "sess-created-lf"},
		TabID:             TabID("tab-created-lf"),
		Rows:              24,
		Cols:              80,
		Activate:          true,
		ScrollbackCapture: []byte("abc\nx"),
	})

	tabs := m.tabsByWorkspace[wsID]
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tabs))
	}
	tab := tabs[0]
	if tab.Terminal == nil {
		t.Fatal("expected terminal to be created")
	}
	if len(tab.Terminal.Scrollback) < 2 {
		t.Fatalf("expected at least 2 scrollback lines, got %d", len(tab.Terminal.Scrollback))
	}
	if got := tab.Terminal.Scrollback[1][0].Rune; got != 'x' {
		t.Fatalf("expected captured scrollback LF to reset to col 0, got %q", got)
	}
}

func TestHandlePtyTabCreated_ExistingResetsActivityANSIState(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                TabID("tab-1"),
		Assistant:         "codex",
		Workspace:         ws,
		activityANSIState: ansiActivityOSC,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_ = m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace: ws,
		Assistant: "codex",
		Agent:     &appPty.Agent{Session: "sess-created"},
		TabID:     tab.ID,
		Rows:      24,
		Cols:      80,
		Activate:  true,
	})

	if tab.activityANSIState != ansiActivityText {
		t.Fatalf("expected activityANSIState reset to text on existing tab create path, got %v", tab.activityANSIState)
	}
}

func TestHandlePtyTabCreated_RejectsMissingTabID(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())

	cmd := m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace: ws,
		Assistant: "codex",
		Agent:     &appPty.Agent{Session: "sess-missing-id"},
		Rows:      24,
		Cols:      80,
		Activate:  true,
	})
	if cmd == nil {
		t.Fatal("expected error command for missing tab id")
	}

	msg := cmd()
	errMsg, ok := msg.(messages.Error)
	if !ok {
		t.Fatalf("expected messages.Error, got %T", msg)
	}
	if errMsg.Err == nil || errMsg.Err.Error() != "missing tab id" {
		t.Fatalf("expected missing tab id error, got %v", errMsg.Err)
	}
	if len(m.tabsByWorkspace[wsID]) != 0 {
		t.Fatalf("expected no tabs to be created on missing tab id, got %d", len(m.tabsByWorkspace[wsID]))
	}
}

func TestHandlePtyTabCreated_ExistingResetsStableCursor(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                TabID("tab-1"),
		Assistant:         "codex",
		Workspace:         ws,
		Terminal:          vterm.New(80, 24),
		stableCursorSet:   true,
		stableCursorX:     7,
		stableCursorY:     20,
		lastOutputAt:      time.Now(),
		lastUserInputAt:   time.Now(),
		lastPromptInputAt: time.Now(),
		lastVisibleOutput: time.Now(),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_ = m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace: ws,
		Assistant: "codex",
		Agent:     &appPty.Agent{Session: "sess-created"},
		TabID:     tab.ID,
		Rows:      24,
		Cols:      80,
		Activate:  true,
	})

	if tab.stableCursorSet {
		t.Fatal("expected stable cursor to clear on existing tab create path")
	}
	if tab.stableCursorX != 0 || tab.stableCursorY != 0 {
		t.Fatalf("expected cleared stable cursor coordinates, got (%d,%d)", tab.stableCursorX, tab.stableCursorY)
	}
	if !tab.lastUserInputAt.IsZero() {
		t.Fatal("expected recent-input state to clear on existing tab create path")
	}
	if !tab.lastPromptInputAt.IsZero() {
		t.Fatal("expected recent prompt-input state to clear on existing tab create path")
	}
	if !tab.lastVisibleOutput.IsZero() {
		t.Fatal("expected visible-activity state to clear on existing tab create path")
	}
	if !tab.lastOutputAt.IsZero() {
		t.Fatal("expected recent output state to clear on existing tab create path")
	}
}

func TestUpdatePTYStopped_ResetsActivityANSIState(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                TabID("tab-1"),
		Assistant:         "codex",
		Workspace:         ws,
		activityANSIState: ansiActivityOSC,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_ = m.updatePTYStopped(PTYStopped{
		WorkspaceID: wsID,
		TabID:       tab.ID,
	})

	if tab.activityANSIState != ansiActivityText {
		t.Fatalf("expected activityANSIState reset to text on PTY stop, got %v", tab.activityANSIState)
	}
}

func TestUpdatePTYRestart_ResetsActivityANSIState(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                TabID("tab-1"),
		Assistant:         "codex",
		Workspace:         ws,
		activityANSIState: ansiActivityCSI,
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_ = m.updatePTYRestart(PTYRestart{
		WorkspaceID: wsID,
		TabID:       tab.ID,
	})

	if tab.activityANSIState != ansiActivityText {
		t.Fatalf("expected activityANSIState reset to text on PTY restart, got %v", tab.activityANSIState)
	}
}
