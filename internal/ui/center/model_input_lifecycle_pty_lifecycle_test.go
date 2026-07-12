package center

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
	"github.com/andyrewlee/amux/internal/vterm"
)

func TestUpdatePtyTabReattachResult_ResetsActivityANSIState(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-1"),
		Assistant: "codex",
		Workspace: ws,
		tabActivityState: tabActivityState{
			activityANSIState: ansiActivityString,
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

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
		ID:        TabID("tab-1"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
		tabCursorState: tabCursorState{
			stableCursorSet: true,
			stableCursorX:   7,
			stableCursorY:   20,
		},
		State: ptyio.State{
			LastOutputAt: time.Now(),
		},
		tabActivityState: tabActivityState{
			lastUserInputAt:   time.Now(),
			lastPromptInputAt: time.Now(),
			lastVisibleOutput: time.Now(),
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

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
	if !tab.LastOutputAt.IsZero() {
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
		ID:        TabID("tab-reattach-carry"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  term,
		State: ptyio.State{
			PendingOutput: []byte("[31mHello"),
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

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
	if got := string(tab.PendingOutput); got != "[31mHello" {
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
		State: ptyio.State{
			PendingOutput: []byte("buffered"),
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

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
		ID:        TabID("tab-1"),
		Assistant: "codex",
		Workspace: ws,
		tabActivityState: tabActivityState{
			activityANSIState: ansiActivityOSC,
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

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
		State: ptyio.State{
			PendingOutput: []byte("buffered"),
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

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
		ID:        TabID("tab-created-carry"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  term,
		State: ptyio.State{
			PendingOutput: []byte("[31mHello"),
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

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
	if got := string(tab.PendingOutput); got != "[31mHello" {
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
	if len(m.tabs.ByWorkspace[wsID]) != 0 {
		t.Fatalf("expected no tabs to be created on missing tab id, got %d", len(m.tabs.ByWorkspace[wsID]))
	}
}

func TestHandlePtyTabCreated_ExistingResetsStableCursor(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-1"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
		tabCursorState: tabCursorState{
			stableCursorSet: true,
			stableCursorX:   7,
			stableCursorY:   20,
		},
		State: ptyio.State{
			LastOutputAt: time.Now(),
		},
		tabActivityState: tabActivityState{
			lastUserInputAt:   time.Now(),
			lastPromptInputAt: time.Now(),
			lastVisibleOutput: time.Now(),
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

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
	if !tab.LastOutputAt.IsZero() {
		t.Fatal("expected recent output state to clear on existing tab create path")
	}
}

func TestUpdatePTYStopped_ResetsActivityANSIState(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-1"),
		Assistant: "codex",
		Workspace: ws,
		tabActivityState: tabActivityState{
			activityANSIState: ansiActivityOSC,
		},
		State: ptyio.State{
			OverflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryCSI},
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

	_ = m.updatePTYStopped(PTYStopped{
		WorkspaceID: wsID,
		TabID:       tab.ID,
	})

	if tab.activityANSIState != ansiActivityText {
		t.Fatalf("expected activityANSIState reset to text on PTY stop, got %v", tab.activityANSIState)
	}
	if tab.OverflowTrimCarry != (vterm.ParserCarryState{Mode: vterm.ParserCarryCSI}) {
		t.Fatalf("expected overflowTrimCarry preserved on PTY stop, got %+v", tab.OverflowTrimCarry)
	}

	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("31mHello"),
	})
	if string(tab.PendingOutput) != "Hello" {
		t.Fatalf("expected post-stop continuation to trim to visible text, got %q", tab.PendingOutput)
	}
}

func TestUpdatePTYRestart_ResetsActivityANSIState(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-1"),
		Assistant: "codex",
		Workspace: ws,
		tabActivityState: tabActivityState{
			activityANSIState: ansiActivityCSI,
		},
		State: ptyio.State{
			OverflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryCSI},
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

	_ = m.updatePTYRestart(PTYRestart{
		WorkspaceID: wsID,
		TabID:       tab.ID,
	})

	if tab.activityANSIState != ansiActivityText {
		t.Fatalf("expected activityANSIState reset to text on PTY restart, got %v", tab.activityANSIState)
	}
	if tab.OverflowTrimCarry != (vterm.ParserCarryState{Mode: vterm.ParserCarryCSI}) {
		t.Fatalf("expected overflowTrimCarry preserved on PTY restart, got %+v", tab.OverflowTrimCarry)
	}

	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("31mHello"),
	})
	if string(tab.PendingOutput) != "Hello" {
		t.Fatalf("expected post-restart continuation to trim to visible text, got %q", tab.PendingOutput)
	}
}

func TestUpdatePTYStopped_TrimsSecondaryDAContinuationAfterEscapeCarry(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-da-stop"),
		Assistant: "codex",
		Workspace: ws,
		State: ptyio.State{
			OverflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryEscape},
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

	_ = m.updatePTYStopped(PTYStopped{WorkspaceID: wsID, TabID: tab.ID})
	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("[>1;10;0cvisible"),
	})

	if string(tab.PendingOutput) != "visible" {
		t.Fatalf("expected secondary DA continuation trimmed after stop, got %q", tab.PendingOutput)
	}
}

// livePTYTab returns a center tab whose Agent.Terminal is a zero-value
// *pty.Terminal. A freshly constructed terminal has closed==false, so
// IsClosed() reports false and updatePTYStopped takes the termAlive
// restart-backoff branch. This is the same fake the sidebar restart tests
// (terminal_update_pty_test.go) rely on.
func livePTYTab(id TabID, ws *data.Workspace) *Tab {
	return &Tab{
		ID:        id,
		Assistant: "codex",
		Workspace: ws,
		Agent:     &appPty.Agent{Terminal: &appPty.Terminal{}},
		Running:   true,
	}
}

func TestUpdatePTYStopped_RestartSchedulesTickUnderLimit(t *testing.T) {
	if (&appPty.Terminal{}).IsClosed() {
		t.Fatal("precondition: a zero-value *pty.Terminal must report IsClosed()==false")
	}

	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := livePTYTab(TabID("tab-restart"), ws)
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

	// Each of the first ptyRestartMax stops must schedule a restart tick and
	// leave the tab attached. Backoff doubles until it saturates at the cap,
	// so it must stay positive and non-decreasing across calls.
	lastBackoff := time.Duration(-1) // sentinel below any real backoff
	for i := 0; i < ptyRestartMax; i++ {
		cmd := m.updatePTYStopped(PTYStopped{WorkspaceID: wsID, TabID: tab.ID})
		if cmd == nil {
			t.Fatalf("call %d: expected a non-nil restart tick cmd while under the limit", i+1)
		}
		if i == 0 {
			// Drive the first tick (shortest backoff) to prove the batch
			// really carries a PTYRestart for this tab, not just any cmd.
			var restart PTYRestart
			found := false
			for _, msg := range drainBatch(cmd) {
				if r, ok := msg.(PTYRestart); ok {
					restart = r
					found = true
					break
				}
			}
			if !found {
				t.Fatal("expected the restart tick to produce a PTYRestart message")
			}
			if restart.WorkspaceID != wsID || restart.TabID != tab.ID {
				t.Fatalf("expected PTYRestart for %s/%s, got %+v", wsID, tab.ID, restart)
			}
		}
		if tab.Detached {
			t.Fatalf("call %d: expected Detached==false while restarting, got true", i+1)
		}
		if !tab.Running {
			t.Fatalf("call %d: expected Running to stay true while restarting", i+1)
		}
		got := tab.RestartBackoff
		if got <= 0 {
			t.Fatalf("call %d: expected positive backoff, got %v", i+1, got)
		}
		if got < lastBackoff {
			t.Fatalf("call %d: expected backoff to grow or hold, got %v after %v", i+1, got, lastBackoff)
		}
		lastBackoff = got
	}
}

func TestUpdatePTYStopped_DetachesAfterRestartLimit(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := livePTYTab(TabID("tab-restart-limit"), ws)
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

	// Exhaust the restart budget; these all schedule ticks (not driven).
	for i := 0; i < ptyRestartMax; i++ {
		if cmd := m.updatePTYStopped(PTYStopped{WorkspaceID: wsID, TabID: tab.ID}); cmd == nil {
			t.Fatalf("call %d: expected restart tick while under the limit", i+1)
		}
	}
	if tab.Detached {
		t.Fatal("expected tab to remain attached up to the restart limit")
	}

	// One more stop within the window exceeds ptyRestartMax: the handler
	// gives up, marks the tab detached, and emits TabStateChanged with no
	// restart tick.
	cmd := m.updatePTYStopped(PTYStopped{WorkspaceID: wsID, TabID: tab.ID})
	if cmd == nil {
		t.Fatal("expected a TabStateChanged cmd once the restart limit is exceeded")
	}
	if !tab.Detached {
		t.Fatal("expected Detached==true after exceeding the restart limit")
	}
	if tab.Running {
		t.Fatal("expected Running==false after exceeding the restart limit")
	}
	stateChanged := false
	for _, msg := range drainBatch(cmd) {
		switch got := msg.(type) {
		case PTYRestart:
			t.Fatalf("expected no restart tick after the limit, got %+v", got)
		case messages.TabStateChanged:
			if got.WorkspaceID != wsID || got.TabID != string(tab.ID) {
				t.Fatalf("expected TabStateChanged for %s/%s, got %+v", wsID, tab.ID, got)
			}
			stateChanged = true
		}
	}
	if !stateChanged {
		t.Fatal("expected TabStateChanged after exceeding the restart limit")
	}
}

func TestUpdatePTYRestart_TrimsSecondaryDAContinuationAfterEscapeCarry(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:        TabID("tab-da-restart"),
		Assistant: "codex",
		Workspace: ws,
		State: ptyio.State{
			OverflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryEscape},
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}

	_ = m.updatePTYRestart(PTYRestart{WorkspaceID: wsID, TabID: tab.ID})
	_ = m.updatePTYOutput(PTYOutput{
		WorkspaceID: wsID,
		TabID:       tab.ID,
		Data:        []byte("[>1;10;0cvisible"),
	})

	if string(tab.PendingOutput) != "visible" {
		t.Fatalf("expected secondary DA continuation trimmed after restart, got %q", tab.PendingOutput)
	}
}
