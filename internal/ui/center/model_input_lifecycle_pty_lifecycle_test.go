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

func TestUpdatePtyTabReattachResult_PreservesParserCarryOnExistingTerminal(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(80, 24)
	term.Write([]byte{0x1b})
	tab := &Tab{
		ID:            TabID("tab-reattach-carry"),
		Assistant:     "codex",
		Workspace:     ws,
		Terminal:      term,
		pendingOutput: []byte("[31mHello"),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	if got := tab.Terminal.ParserCarryState(); got != (vterm.ParserCarryState{Mode: vterm.ParserCarryEscape}) {
		t.Fatalf("expected precondition escape carry, got %+v", got)
	}

	_, _ = m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Agent:       &appPty.Agent{Session: "sess-reattach-carry"},
		Rows:        24,
		Cols:        80,
	})

	if got := tab.Terminal.ParserCarryState(); got != (vterm.ParserCarryState{Mode: vterm.ParserCarryEscape}) {
		t.Fatalf("expected escape carry preserved on reattach, got %+v", got)
	}
	if got := tab.actorQueuedCarry; got != (vterm.ParserCarryState{Mode: vterm.ParserCarryEscape}) {
		t.Fatalf("expected actor queued carry to match preserved parser carry, got %+v", got)
	}
	if got := string(tab.pendingOutput); got != "[31mHello" {
		t.Fatalf("expected buffered continuation bytes to survive reattach, got %q", got)
	}
}

func TestUpdatePtyTabReattachResult_ClearsCatchUpPendingOutput(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                   TabID("tab-reattach-catch-up"),
		Assistant:            "codex",
		Workspace:            ws,
		Terminal:             vterm.New(80, 24),
		catchUpPendingOutput: true,
		pendingOutput:        []byte("buffered"),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_, _ = m.updatePtyTabReattachResult(ptyTabReattachResult{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Agent:       &appPty.Agent{Session: "sess-reattach-catch-up"},
		Rows:        24,
		Cols:        80,
	})

	if tab.catchUpPendingOutput {
		t.Fatal("expected catch-up latch to clear on reattach")
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

func TestHandlePtyTabCreated_ExistingClearsCatchUpPendingOutput(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                   TabID("tab-created-catch-up"),
		Assistant:            "codex",
		Workspace:            ws,
		Terminal:             vterm.New(80, 24),
		catchUpPendingOutput: true,
		pendingOutput:        []byte("buffered"),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_ = m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace: ws,
		Assistant: "codex",
		Agent:     &appPty.Agent{Session: "sess-created-catch-up"},
		TabID:     tab.ID,
		Rows:      24,
		Cols:      80,
		Activate:  true,
	})

	if tab.catchUpPendingOutput {
		t.Fatal("expected catch-up latch to clear on existing tab create path")
	}
}

func TestHandlePtyTabCreated_ExistingPreservesParserCarry(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	term := vterm.New(80, 24)
	term.Write([]byte{0x1b})
	tab := &Tab{
		ID:            TabID("tab-created-carry"),
		Assistant:     "codex",
		Workspace:     ws,
		Terminal:      term,
		pendingOutput: []byte("[31mHello"),
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	if got := tab.Terminal.ParserCarryState(); got != (vterm.ParserCarryState{Mode: vterm.ParserCarryEscape}) {
		t.Fatalf("expected precondition escape carry, got %+v", got)
	}

	_ = m.handlePtyTabCreated(ptyTabCreateResult{
		Workspace: ws,
		Assistant: "codex",
		Agent:     &appPty.Agent{Session: "sess-created-carry"},
		TabID:     tab.ID,
		Rows:      24,
		Cols:      80,
		Activate:  true,
	})

	if got := tab.Terminal.ParserCarryState(); got != (vterm.ParserCarryState{Mode: vterm.ParserCarryEscape}) {
		t.Fatalf("expected escape carry preserved on existing create path, got %+v", got)
	}
	if got := tab.actorQueuedCarry; got != (vterm.ParserCarryState{Mode: vterm.ParserCarryEscape}) {
		t.Fatalf("expected actor queued carry to match preserved parser carry, got %+v", got)
	}
	if got := string(tab.pendingOutput); got != "[31mHello" {
		t.Fatalf("expected buffered continuation bytes to survive existing create path, got %q", got)
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
		overflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryCSI},
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_ = m.updatePTYStopped(PTYStopped{
		WorkspaceID: wsID,
		TabID:       tab.ID,
	})

	if tab.activityANSIState != ansiActivityText {
		t.Fatalf("expected activityANSIState reset to text on PTY stop, got %v", tab.activityANSIState)
	}
	if tab.overflowTrimCarry != (vterm.ParserCarryState{Mode: vterm.ParserCarryCSI}) {
		t.Fatalf("expected overflowTrimCarry preserved on PTY stop, got %+v", tab.overflowTrimCarry)
	}

	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("31mHello"),
	})
	if string(tab.pendingOutput) != "Hello" {
		t.Fatalf("expected post-stop continuation to trim to visible text, got %q", tab.pendingOutput)
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
		overflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryCSI},
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_ = m.updatePTYRestart(PTYRestart{
		WorkspaceID: wsID,
		TabID:       tab.ID,
	})

	if tab.activityANSIState != ansiActivityText {
		t.Fatalf("expected activityANSIState reset to text on PTY restart, got %v", tab.activityANSIState)
	}
	if tab.overflowTrimCarry != (vterm.ParserCarryState{Mode: vterm.ParserCarryCSI}) {
		t.Fatalf("expected overflowTrimCarry preserved on PTY restart, got %+v", tab.overflowTrimCarry)
	}

	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("31mHello"),
	})
	if string(tab.pendingOutput) != "Hello" {
		t.Fatalf("expected post-restart continuation to trim to visible text, got %q", tab.pendingOutput)
	}
}

func TestUpdatePTYStopped_TrimsSecondaryDAContinuationAfterEscapeCarry(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                TabID("tab-da-stop"),
		Assistant:         "codex",
		Workspace:         ws,
		overflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryEscape},
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_ = m.updatePTYStopped(PTYStopped{WorkspaceID: wsID, TabID: tab.ID})
	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("[>1;10;0cvisible"),
	})

	if string(tab.pendingOutput) != "visible" {
		t.Fatalf("expected secondary DA continuation trimmed after stop, got %q", tab.pendingOutput)
	}
}

func TestUpdatePTYRestart_TrimsSecondaryDAContinuationAfterEscapeCarry(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:                TabID("tab-da-restart"),
		Assistant:         "codex",
		Workspace:         ws,
		overflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryEscape},
	}
	m.tabsByWorkspace[wsID] = []*Tab{tab}

	_ = m.updatePTYRestart(PTYRestart{WorkspaceID: wsID, TabID: tab.ID})
	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("[>1;10;0cvisible"),
	})

	if string(tab.pendingOutput) != "visible" {
		t.Fatalf("expected secondary DA continuation trimmed after restart, got %q", tab.pendingOutput)
	}
}
