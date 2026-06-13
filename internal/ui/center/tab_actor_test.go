package center

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	appPty "github.com/andyrewlee/amux/internal/pty"
)

func TestSendTabEvent_ClosedWriteOutputReturnsFalse(t *testing.T) {
	m := &Model{tabEvents: make(chan tabEvent, 1)}
	tab := &Tab{}
	tab.markClosed()

	if m.sendTabEvent(tabEvent{tab: tab, kind: tabEventWriteOutput}) {
		t.Fatal("expected closed write output event to report enqueue failure")
	}
	if got := len(m.tabEvents); got != 0 {
		t.Fatalf("expected no queued events, got %d", got)
	}
}

func TestSendTabEvent_ClosedNonWriteOutputReturnsTrue(t *testing.T) {
	m := &Model{tabEvents: make(chan tabEvent, 1)}
	tab := &Tab{}
	tab.markClosed()

	if !m.sendTabEvent(tabEvent{tab: tab, kind: tabEventSelectionClear}) {
		t.Fatal("expected closed non-write event to be treated as handled")
	}
	if got := len(m.tabEvents); got != 0 {
		t.Fatalf("expected no queued events, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Step 1: pure-function tables
// ---------------------------------------------------------------------------

// fillTabEvents pre-loads ch with n dummy events so cap/len thresholds in
// shouldDropTabEvent can be hit exactly.
func fillTabEvents(ch chan tabEvent, n int) {
	for i := 0; i < n; i++ {
		ch <- tabEvent{kind: tabEventWriteOutput}
	}
}

func TestShouldDropTabEvent(t *testing.T) {
	// cap=8 ⇒ threshold len >= (8*3)/4 = 6.
	droppableKinds := []tabEventKind{
		tabEventSelectionUpdate,
		tabEventSelectionScrollTick,
		tabEventScrollBy,
		tabEventScrollPage,
	}

	t.Run("nil channel drops", func(t *testing.T) {
		if !shouldDropTabEvent(nil, tabEventSelectionUpdate) {
			t.Fatal("nil channel should always drop")
		}
		// Even a non-droppable kind drops on a nil channel.
		if !shouldDropTabEvent(nil, tabEventSendInput) {
			t.Fatal("nil channel should drop regardless of kind")
		}
	})

	t.Run("zero-cap channel keeps", func(t *testing.T) {
		ch := make(chan tabEvent)
		if shouldDropTabEvent(ch, tabEventSelectionUpdate) {
			t.Fatal("zero-capacity channel should never drop")
		}
	})

	t.Run("droppable kinds at >= 3/4 fill drop", func(t *testing.T) {
		for _, kind := range droppableKinds {
			ch := make(chan tabEvent, 8)
			fillTabEvents(ch, 6) // exactly the threshold
			if !shouldDropTabEvent(ch, kind) {
				t.Fatalf("kind %d at len=6 cap=8 should drop", kind)
			}
		}
	})

	t.Run("droppable kinds below threshold keep", func(t *testing.T) {
		for _, kind := range droppableKinds {
			ch := make(chan tabEvent, 8)
			fillTabEvents(ch, 5) // one below the threshold
			if shouldDropTabEvent(ch, kind) {
				t.Fatalf("kind %d at len=5 cap=8 should not drop", kind)
			}
		}
	})

	t.Run("non-droppable kind at full keeps", func(t *testing.T) {
		ch := make(chan tabEvent, 8)
		fillTabEvents(ch, 8) // completely full
		if shouldDropTabEvent(ch, tabEventSendInput) {
			t.Fatal("non-droppable kind must never be dropped, even when full")
		}
	})
}

// TestShouldPostTabActorRedraw_AllKinds is the exhaustive complement to the
// partial table in tab_actor_input_test.go: it asserts the redraw partition
// across every one of the 16 tabEventKind values.
func TestShouldPostTabActorRedraw_AllKinds(t *testing.T) {
	cases := []struct {
		kind tabEventKind
		want bool
	}{
		// The 10 redraw kinds.
		{tabEventSelectionStart, true},
		{tabEventSelectionUpdate, true},
		{tabEventSelectionFinish, true},
		{tabEventScrollBy, true},
		{tabEventSelectionClearAndNotify, true},
		{tabEventSelectionScrollTick, true},
		{tabEventScrollToBottom, true},
		{tabEventScrollPage, true},
		{tabEventScrollToTop, true},
		{tabEventDiffInput, true},
		// The 6 non-redraw kinds.
		{tabEventSelectionClear, false},
		{tabEventSelectionCopy, false},
		{tabEventSendInput, false},
		{tabEventSendMouse, false},
		{tabEventPaste, false},
		{tabEventWriteOutput, false},
	}
	for _, tc := range cases {
		if got := shouldPostTabActorRedraw(tc.kind); got != tc.want {
			t.Errorf("shouldPostTabActorRedraw(kind=%d) = %v, want %v", tc.kind, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Step 2: sendTabEvent coverage beyond the two closed-tab cases
// ---------------------------------------------------------------------------

func TestSendTabEvent_NilModelOrChannel(t *testing.T) {
	var nilModel *Model
	if nilModel.sendTabEvent(tabEvent{kind: tabEventSendInput}) {
		t.Fatal("nil model should report enqueue failure")
	}

	m := &Model{} // tabEvents is nil
	if m.sendTabEvent(tabEvent{kind: tabEventSendInput}) {
		t.Fatal("nil tabEvents channel should report enqueue failure")
	}
}

func TestSendTabEvent_NilTabDrops(t *testing.T) {
	m := &Model{tabEvents: make(chan tabEvent, 1)}
	if m.sendTabEvent(tabEvent{kind: tabEventSendInput}) {
		t.Fatal("nil tab should report enqueue failure")
	}
	if got := len(m.tabEvents); got != 0 {
		t.Fatalf("expected nothing queued for nil tab, got %d", got)
	}
}

func TestSendTabEvent_OpenTabRoomEnqueues(t *testing.T) {
	m := &Model{tabEvents: make(chan tabEvent, 1)}
	tab := &Tab{}
	if !m.sendTabEvent(tabEvent{tab: tab, kind: tabEventSendInput}) {
		t.Fatal("open tab with channel room should enqueue and report success")
	}
	if got := len(m.tabEvents); got != 1 {
		t.Fatalf("expected exactly 1 queued event, got %d", got)
	}
}

func TestSendTabEvent_FullChannelNonDroppableOverflows(t *testing.T) {
	m := &Model{tabEvents: make(chan tabEvent, 1)}
	tab := &Tab{}
	// Pre-fill the only slot so the next send overflows.
	m.tabEvents <- tabEvent{kind: tabEventWriteOutput}

	if m.sendTabEvent(tabEvent{tab: tab, kind: tabEventSendInput}) {
		t.Fatal("send into a full channel must report failure (overflow drop)")
	}
	if got := len(m.tabEvents); got != 1 {
		t.Fatalf("expected channel still full at len 1, got %d", got)
	}
}

func TestSendTabEvent_DroppableKindBackpressureDrops(t *testing.T) {
	// cap=8, threshold at len>=6. Fill to 6 so a droppable kind is shed by
	// backpressure (distinct from overflow: the channel is not yet full).
	m := &Model{tabEvents: make(chan tabEvent, 8)}
	tab := &Tab{}
	fillTabEvents(m.tabEvents, 6)

	if m.sendTabEvent(tabEvent{tab: tab, kind: tabEventScrollBy}) {
		t.Fatal("droppable kind at >=3/4 fill should be shed by backpressure")
	}
	if got := len(m.tabEvents); got != 6 {
		t.Fatalf("expected length unchanged at 6 after backpressure drop, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Step 3: RunTabActor lifecycle
// ---------------------------------------------------------------------------

func TestRunTabActor_NilModelOrChannelReturnsImmediately(t *testing.T) {
	var nilModel *Model
	if err := nilModel.RunTabActor(context.Background()); err != nil {
		t.Fatalf("nil model should return nil, got %v", err)
	}

	m := &Model{} // tabEvents is nil
	if err := m.RunTabActor(context.Background()); err != nil {
		t.Fatalf("nil channel should return nil, got %v", err)
	}
}

func TestRunTabActor_ContextCancellationReturns(t *testing.T) {
	m := &Model{tabEvents: make(chan tabEvent, 1)}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- m.RunTabActor(ctx) }()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunTabActor returned non-nil on cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunTabActor did not return after context cancellation")
	}
}

func TestRunTabActor_ConsumesEnqueuedEvent(t *testing.T) {
	// A minimal Tab is sufficient for handleSelectionClear: with nil Terminal
	// and a zero-value Selection it takes the no-redraw branch and returns.
	m := &Model{tabEvents: make(chan tabEvent, 1)}
	tab := &Tab{}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.RunTabActor(ctx) }()

	// Enqueue one event for the actor to consume.
	m.tabEvents <- tabEvent{tab: tab, kind: tabEventSelectionClear}

	// Wait until the actor has drained the channel, then cancel and join. The
	// poll is a synchronization point, not a fixed sleep.
	deadline := time.Now().Add(2 * time.Second)
	for len(m.tabEvents) > 0 {
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatal("actor did not consume the enqueued event")
		}
		time.Sleep(time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunTabActor did not return after consuming event")
	}
}

// ---------------------------------------------------------------------------
// Step 4: sendToTerminal failure-detach contract (msgSink)
// ---------------------------------------------------------------------------

// TestSendToTerminal_FailureDetachesAndNotifies characterizes the keystroke
// failure path: when Terminal.SendString errors, sendToTerminal marks the tab
// detached (Running=false, Detached=true under tab.mu) and emits a
// TabInputFailed carrying the originating TabID/WorkspaceID through msgSink.
//
// A real *pty.Terminal cannot be struct-literal-constructed from this package
// (unexported fields), so we spawn one and Close it: a closed terminal's
// SendString returns io.ErrClosedPipe without keeping any agent process alive.
func TestSendToTerminal_FailureDetachesAndNotifies(t *testing.T) {
	dir := t.TempDir()
	term, err := appPty.NewWithSize("cat >/dev/null", dir, nil, 24, 80)
	if err != nil {
		t.Fatalf("expected test PTY terminal: %v", err)
	}
	// Closing first makes the subsequent SendString fail deterministically.
	if err := term.Close(); err != nil {
		t.Fatalf("close terminal: %v", err)
	}

	tabID := TabID("tab-send-fail")
	workspaceID := "ws-send-fail"
	tab := &Tab{
		ID: tabID,
		// A non-chat assistant keeps the path simple; the failure return is
		// reached before any chat-only PTYCursorRefresh anyway.
		Assistant: "not-a-chat-assistant",
		Agent:     &appPty.Agent{Terminal: term},
		Running:   true,
	}

	var got []tea.Msg
	m := &Model{}
	m.msgSink = func(msg tea.Msg) { got = append(got, msg) }

	m.sendToTerminal(tab, "x", tabID, workspaceID, "Input")

	tab.mu.Lock()
	detached := tab.Detached
	running := tab.Running
	tab.mu.Unlock()
	if !detached {
		t.Fatal("expected tab.Detached=true after SendString failure")
	}
	if running {
		t.Fatal("expected tab.Running=false after SendString failure")
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
