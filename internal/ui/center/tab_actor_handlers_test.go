package center

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/diff"
	"github.com/andyrewlee/amux/internal/vterm"
)

// ---------------------------------------------------------------------------
// tabActorRedraw marker methods
// ---------------------------------------------------------------------------

// TestTabActorRedraw_MarkerMethods exercises the two marker methods directly.
// They are intentionally no-ops, but the compiler-level guarantee that they
// implement the critical-external-msg contract is the load-bearing behavior:
// the actor relies on these markers so its redraw nudge survives msg eviction.
func TestTabActorRedraw_MarkerMethods(t *testing.T) {
	r := tabActorRedraw{}
	// Direct calls must not panic and must remain no-ops (nothing to observe).
	// Interface satisfaction is covered by
	// TestTabActorRedraw_IsNonEvictingCriticalExternalMsg.
	r.MarkCriticalExternalMsg()
	r.MarkNonEvictingCriticalExternalMsg()
}

// ---------------------------------------------------------------------------
// scroll handlers: handleScrollBy / handleScrollToBottom / handleScrollToTop
// ---------------------------------------------------------------------------

// newScrolledTerminal builds a vterm with enough output pushed past the visible
// screen that there is real scrollback to move the view across. It returns the
// terminal and its max view offset (>0).
func newScrolledTerminal(t *testing.T) (*vterm.VTerm, int) {
	t.Helper()
	term := vterm.New(20, 4)
	// 12 lines into a 4-row screen leaves 8 lines of scrollback.
	term.Write([]byte("l0\nl1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\n"))
	_, maxOffset := term.GetScrollInfo()
	if maxOffset <= 0 {
		t.Fatalf("expected non-empty scrollback, got maxOffset=%d", maxOffset)
	}
	return term, maxOffset
}

func TestHandleScrollBy(t *testing.T) {
	m := newTestModel()

	t.Run("positive delta scrolls up into history", func(t *testing.T) {
		term, _ := newScrolledTerminal(t)
		tab := &Tab{Terminal: term}
		m.handleScrollBy(tabEvent{tab: tab, delta: 3})
		if got := tab.Terminal.ViewOffset; got != 3 {
			t.Fatalf("expected ViewOffset=3 after scroll-by 3, got %d", got)
		}
		if !tab.Terminal.IsScrolled() {
			t.Fatal("expected terminal to report scrolled after positive delta")
		}
	})

	t.Run("negative delta clamps to live view", func(t *testing.T) {
		term, _ := newScrolledTerminal(t)
		term.ScrollView(2) // start scrolled into history
		tab := &Tab{Terminal: term}
		m.handleScrollBy(tabEvent{tab: tab, delta: -10})
		if got := tab.Terminal.ViewOffset; got != 0 {
			t.Fatalf("expected ViewOffset clamped to 0, got %d", got)
		}
		if tab.Terminal.IsScrolled() {
			t.Fatal("expected terminal back at live view after large negative delta")
		}
	})

	t.Run("zero delta is a no-op", func(t *testing.T) {
		term, _ := newScrolledTerminal(t)
		term.ScrollView(2)
		tab := &Tab{Terminal: term}
		m.handleScrollBy(tabEvent{tab: tab, delta: 0})
		if got := tab.Terminal.ViewOffset; got != 2 {
			t.Fatalf("expected zero delta to leave ViewOffset at 2, got %d", got)
		}
	})

	t.Run("nil terminal is a no-op", func(t *testing.T) {
		tab := &Tab{}
		// Must not panic on a tab without a terminal.
		m.handleScrollBy(tabEvent{tab: tab, delta: 5})
	})

	t.Run("delta beyond max offset clamps to max", func(t *testing.T) {
		term, maxOffset := newScrolledTerminal(t)
		tab := &Tab{Terminal: term}
		m.handleScrollBy(tabEvent{tab: tab, delta: maxOffset + 100})
		if got := tab.Terminal.ViewOffset; got != maxOffset {
			t.Fatalf("expected ViewOffset clamped to max %d, got %d", maxOffset, got)
		}
	})
}

func TestHandleScrollToBottom(t *testing.T) {
	m := newTestModel()

	t.Run("scrolled terminal returns to live view", func(t *testing.T) {
		term, _ := newScrolledTerminal(t)
		term.ScrollView(3)
		if !term.IsScrolled() {
			t.Fatal("precondition: terminal should be scrolled")
		}
		tab := &Tab{Terminal: term}
		m.handleScrollToBottom(tabEvent{tab: tab})
		if got := tab.Terminal.ViewOffset; got != 0 {
			t.Fatalf("expected ViewOffset=0 after scroll-to-bottom, got %d", got)
		}
		if tab.Terminal.IsScrolled() {
			t.Fatal("expected terminal at live view after scroll-to-bottom")
		}
	})

	t.Run("already at bottom is a no-op", func(t *testing.T) {
		term, _ := newScrolledTerminal(t)
		if term.IsScrolled() {
			t.Fatal("precondition: terminal should start at live view")
		}
		tab := &Tab{Terminal: term}
		m.handleScrollToBottom(tabEvent{tab: tab})
		if got := tab.Terminal.ViewOffset; got != 0 {
			t.Fatalf("expected ViewOffset unchanged at 0, got %d", got)
		}
	})

	t.Run("nil terminal is a no-op", func(t *testing.T) {
		m.handleScrollToBottom(tabEvent{tab: &Tab{}})
	})
}

func TestHandleScrollToTop(t *testing.T) {
	t.Run("non-chat tab scrolls to oldest content", func(t *testing.T) {
		m := newTestModel()
		term, maxOffset := newScrolledTerminal(t)
		tab := &Tab{Assistant: "bash", Terminal: term}
		m.handleScrollToTop(tabEvent{tab: tab})
		if got := tab.Terminal.ViewOffset; got != maxOffset {
			t.Fatalf("expected ViewOffset=max %d after scroll-to-top, got %d", maxOffset, got)
		}
		if !tab.Terminal.IsScrolled() {
			t.Fatal("expected terminal scrolled into history after scroll-to-top")
		}
	})

	t.Run("nil terminal is a no-op", func(t *testing.T) {
		m := newTestModel()
		m.handleScrollToTop(tabEvent{tab: &Tab{}})
	})

	t.Run("nil model receiver tolerated", func(t *testing.T) {
		// scrollTerminalToTopLocked guards a nil model (m != nil) before the
		// chat-tab lookup; exercise that guard with a genuinely nil receiver so
		// the lookup is skipped and we fall through to ScrollViewToTop.
		var m *Model
		term, maxOffset := newScrolledTerminal(t)
		tab := &Tab{Assistant: "vim", Terminal: term}
		m.scrollTerminalToTopLocked(tab)
		if got := tab.Terminal.ViewOffset; got != maxOffset {
			t.Fatalf("expected nil-model scroll-to-top to reach max %d, got %d", maxOffset, got)
		}
	})
}

// ---------------------------------------------------------------------------
// handleDiffInput
// ---------------------------------------------------------------------------

func newDiffTab(t *testing.T) (*Tab, *diff.Model) {
	t.Helper()
	ws := newTestWorkspace("ws", t.TempDir())
	dv := diff.New(ws, &git.Change{Path: "main.go", Kind: git.ChangeModified}, git.DiffModeUnstaged, 80, 24)
	tab := &Tab{ID: TabID("tab-diff"), Assistant: "diff", Workspace: ws, DiffViewer: dv}
	return tab, dv
}

func TestHandleDiffInput_NilViewerIsNoOp(t *testing.T) {
	m := newTestModel()
	emitted := 0
	m.msgSink = func(tea.Msg) { emitted++ }

	tab := &Tab{ID: TabID("tab-no-diff")}
	m.handleDiffInput(tabEvent{tab: tab, diffMsg: tea.KeyPressMsg{}})

	if emitted != 0 {
		t.Fatalf("expected no msgSink emissions for nil DiffViewer, got %d", emitted)
	}
	if tab.DiffViewer != nil {
		t.Fatal("expected DiffViewer to remain nil")
	}
}

func TestHandleDiffInput_ForwardsCmdThroughMsgSink(t *testing.T) {
	m := newTestModel()
	var sunk []tea.Msg
	m.msgSink = func(msg tea.Msg) { sunk = append(sunk, msg) }

	tab, dv := newDiffTab(t)
	// A focused diff viewer returns a CloseTab command for the "q" key, which is
	// the only easily-constructable path that yields a non-nil cmd.
	dv.Focus()

	m.handleDiffInput(tabEvent{
		tab:     tab,
		diffMsg: tea.KeyPressMsg{Code: 'q', Text: "q"},
	})

	// The updated viewer must be swapped back into the tab.
	if tab.DiffViewer == nil {
		t.Fatal("expected DiffViewer to remain set after update")
	}

	if len(sunk) != 1 {
		t.Fatalf("expected exactly one msgSink emission, got %d: %#v", len(sunk), sunk)
	}
	wrapped, ok := sunk[0].(tabDiffCmd)
	if !ok {
		t.Fatalf("expected a tabDiffCmd, got %T", sunk[0])
	}
	if wrapped.cmd == nil {
		t.Fatal("expected wrapped diff cmd to be non-nil")
	}
	// Running the wrapped command must yield the diff viewer's CloseTab message.
	if _, ok := wrapped.cmd().(messages.CloseTab); !ok {
		t.Fatalf("expected wrapped cmd to produce messages.CloseTab, got %T", wrapped.cmd())
	}
}

func TestHandleDiffInput_NoCmdEmitsNothing(t *testing.T) {
	m := newTestModel()
	emitted := 0
	m.msgSink = func(tea.Msg) { emitted++ }

	tab, dv := newDiffTab(t)
	dv.Focus()
	// "j" scrolls down: it mutates internal state but returns a nil cmd, so no
	// tabDiffCmd should be funneled through msgSink.
	m.handleDiffInput(tabEvent{
		tab:     tab,
		diffMsg: tea.KeyPressMsg{Code: 'j', Text: "j"},
	})

	if emitted != 0 {
		t.Fatalf("expected no msgSink emission for a nil-cmd update, got %d", emitted)
	}
	if tab.DiffViewer == nil {
		t.Fatal("expected DiffViewer to stay set after a no-cmd update")
	}
}

func TestHandleDiffInput_NilMsgSinkDoesNotPanic(t *testing.T) {
	m := newTestModel()
	m.msgSink = nil

	tab, dv := newDiffTab(t)
	dv.Focus()
	// Produces a non-nil cmd but with no sink to receive it; must not panic.
	m.handleDiffInput(tabEvent{
		tab:     tab,
		diffMsg: tea.KeyPressMsg{Code: 'q', Text: "q"},
	})
	if tab.DiffViewer == nil {
		t.Fatal("expected DiffViewer to stay set even without a msgSink")
	}
}

// ---------------------------------------------------------------------------
// handleSendInput / handleSendMouse / handlePaste / sendMouseToTerminal
//
// These funnel into sendToTerminal / sendMouseToTerminal. The pure no-op
// guards (nil tab, empty payload, nil agent, closed tab) need no live process
// and are tested directly. The failure-detach path uses a closed PTY whose
// SendString fails deterministically (same technique as tab_actor_test.go).
// ---------------------------------------------------------------------------

func TestHandlePaste_WrapsBracketedPasteAndForwards(t *testing.T) {
	dir := t.TempDir()
	term, err := appPty.NewWithSize("cat >/dev/null", dir, nil, 24, 80)
	if err != nil {
		t.Fatalf("expected test PTY terminal: %v", err)
	}
	// Closing first makes SendString fail deterministically, which lets us
	// observe that a non-empty paste actually reached sendToTerminal (it only
	// reports failure when it tried to write).
	if err := term.Close(); err != nil {
		t.Fatalf("close terminal: %v", err)
	}

	tabID := TabID("tab-paste")
	workspaceID := "ws-paste"
	tab := &Tab{
		ID:        tabID,
		Assistant: "bash",
		Agent:     &appPty.Agent{Terminal: term},
		Running:   true,
	}

	var got []tea.Msg
	m := &Model{}
	m.msgSink = func(msg tea.Msg) { got = append(got, msg) }

	m.handlePaste(tabEvent{tab: tab, tabID: tabID, workspaceID: workspaceID, pasteText: "hello"})

	// Non-empty paste reached the terminal: the closed PTY failure detaches the
	// tab and emits TabInputFailed.
	tab.mu.Lock()
	detached := tab.Detached
	tab.mu.Unlock()
	if !detached {
		t.Fatal("expected non-empty paste to reach the terminal and detach on failure")
	}
	if len(got) != 1 {
		t.Fatalf("expected one TabInputFailed, got %d: %#v", len(got), got)
	}
	if _, ok := got[0].(TabInputFailed); !ok {
		t.Fatalf("expected TabInputFailed, got %T", got[0])
	}
}

func TestHandlePaste_EmptyTextIsNoOp(t *testing.T) {
	emitted := 0
	m := &Model{msgSink: func(tea.Msg) { emitted++ }}
	// Empty paste must short-circuit before touching the terminal; a nil-agent
	// tab would otherwise be a safe no-op anyway, so assert no emission at all.
	tab := &Tab{ID: TabID("tab-empty-paste"), Running: true}
	m.handlePaste(tabEvent{tab: tab, pasteText: ""})
	if emitted != 0 {
		t.Fatalf("expected empty paste to emit nothing, got %d", emitted)
	}
}

func TestHandleSendInput_NilAgentIsNoOp(t *testing.T) {
	emitted := 0
	m := &Model{msgSink: func(tea.Msg) { emitted++ }}
	// A running tab with no Agent must be a safe no-op (no panic, no emission).
	tab := &Tab{ID: TabID("tab-no-agent"), Assistant: "bash", Running: true}
	m.handleSendInput(tabEvent{tab: tab, input: []byte("abc")})
	if emitted != 0 {
		t.Fatalf("expected nil-agent send to emit nothing, got %d", emitted)
	}
}

func TestHandleSendInput_EmptyInputIsNoOp(t *testing.T) {
	emitted := 0
	m := &Model{msgSink: func(tea.Msg) { emitted++ }}
	tab := &Tab{ID: TabID("tab-empty-input"), Running: true}
	m.handleSendInput(tabEvent{tab: tab, input: nil})
	if emitted != 0 {
		t.Fatalf("expected empty input to emit nothing, got %d", emitted)
	}
}

func TestHandleSendMouse_NilAndEmptyAreNoOps(t *testing.T) {
	emitted := 0
	m := &Model{msgSink: func(tea.Msg) { emitted++ }}

	// Nil tab.
	m.handleSendMouse(tabEvent{tab: nil, input: []byte("\x1b[M")})
	// Empty input.
	m.handleSendMouse(tabEvent{tab: &Tab{Running: true}, input: nil})
	// Running tab, nil agent.
	m.handleSendMouse(tabEvent{tab: &Tab{Assistant: "bash", Running: true}, input: []byte("\x1b[M")})

	if emitted != 0 {
		t.Fatalf("expected mouse no-op paths to emit nothing, got %d", emitted)
	}
}

func TestSendMouseToTerminal_NoOpGuards(t *testing.T) {
	emitted := 0
	m := &Model{msgSink: func(tea.Msg) { emitted++ }}

	// nil tab
	m.sendMouseToTerminal(nil, "\x1b[M", TabID("x"), "ws")
	// empty data
	m.sendMouseToTerminal(&Tab{Running: true}, "", TabID("x"), "ws")
	// closed tab short-circuits before any send
	closed := &Tab{ID: TabID("closed"), Running: true}
	closed.markClosed()
	m.sendMouseToTerminal(closed, "\x1b[M", TabID("closed"), "ws")
	// running tab, nil agent
	m.sendMouseToTerminal(&Tab{Assistant: "bash", Running: true}, "\x1b[M", TabID("x"), "ws")

	if emitted != 0 {
		t.Fatalf("expected guard paths to emit nothing, got %d", emitted)
	}
}

func TestSendMouseToTerminal_FailureDetachesAndNotifies(t *testing.T) {
	dir := t.TempDir()
	term, err := appPty.NewWithSize("cat >/dev/null", dir, nil, 24, 80)
	if err != nil {
		t.Fatalf("expected test PTY terminal: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Fatalf("close terminal: %v", err)
	}

	tabID := TabID("tab-mouse-fail")
	workspaceID := "ws-mouse-fail"
	tab := &Tab{
		ID:        tabID,
		Assistant: "bash",
		Agent:     &appPty.Agent{Terminal: term},
		Running:   true,
	}

	var got []tea.Msg
	m := &Model{msgSink: func(msg tea.Msg) { got = append(got, msg) }}

	m.sendMouseToTerminal(tab, "\x1b[M abc", tabID, workspaceID)

	tab.mu.Lock()
	detached := tab.Detached
	running := tab.Running
	tab.mu.Unlock()
	if !detached {
		t.Fatal("expected tab.Detached=true after mouse SendString failure")
	}
	if running {
		t.Fatal("expected tab.Running=false after mouse SendString failure")
	}

	if len(got) != 1 {
		t.Fatalf("expected exactly one msgSink message, got %d: %#v", len(got), got)
	}
	failed, ok := got[0].(TabInputFailed)
	if !ok {
		t.Fatalf("expected TabInputFailed, got %T", got[0])
	}
	if failed.TabID != tabID {
		t.Errorf("TabInputFailed.TabID = %q, want %q", failed.TabID, tabID)
	}
	if failed.WorkspaceID != workspaceID {
		t.Errorf("TabInputFailed.WorkspaceID = %q, want %q", failed.WorkspaceID, workspaceID)
	}
	if failed.Err == nil {
		t.Error("expected TabInputFailed.Err to be set")
	}
}

// TestHandleSendMouse_SuccessfulSendDoesNotEmitCursorRefresh confirms the mouse
// path never emits the chat-only PTYCursorRefresh that sendToTerminal does: a
// live PTY accepts the bytes and the funnel returns without notifying the sink.
func TestHandleSendMouse_SuccessfulSendDoesNotEmitCursorRefresh(t *testing.T) {
	dir := t.TempDir()
	term, err := appPty.NewWithSize("cat >/dev/null", dir, nil, 24, 80)
	if err != nil {
		t.Fatalf("expected test PTY terminal: %v", err)
	}
	defer func() { _ = term.Close() }()

	tabID := TabID("tab-mouse-ok")
	tab := &Tab{
		ID:        tabID,
		Assistant: "codex", // a chat assistant — proves the mouse path still skips refresh
		Agent:     &appPty.Agent{Terminal: term},
		Running:   true,
	}

	emitted := 0
	m := &Model{msgSink: func(tea.Msg) { emitted++ }}

	m.handleSendMouse(tabEvent{tab: tab, tabID: tabID, workspaceID: "ws", input: []byte("\x1b[M abc")})

	tab.mu.Lock()
	detached := tab.Detached
	tab.mu.Unlock()
	if detached {
		t.Fatal("did not expect a successful mouse send to detach the tab")
	}
	if emitted != 0 {
		t.Fatalf("expected successful mouse send to emit nothing, got %d", emitted)
	}
}
