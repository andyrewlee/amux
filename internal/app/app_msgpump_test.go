package app

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

type testMsg string

type criticalTestMsg struct{}

func (criticalTestMsg) MarkCriticalExternalMsg() {}

var _ common.CriticalExternalMsg = criticalTestMsg{}

func TestEnqueueExternalMsgDropsWhenFull(t *testing.T) {
	a := &App{externalMsgs: make(chan tea.Msg, 1)}

	msg1 := testMsg("first")
	msg2 := testMsg("second")

	a.enqueueExternalMsg(msg1)

	attempted := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(attempted)
		a.enqueueExternalMsg(msg2)
		close(done)
	}()
	<-attempted

	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("expected enqueue to return quickly when external queue is full")
	}

	sent := make(chan tea.Msg, 2)
	a.SetMsgSender(func(msg tea.Msg) { sent <- msg })

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected enqueue to unblock after drain starts")
	}

	if got := readMsg(t, sent); got != msg1 {
		t.Fatalf("expected first message %q, got %q", msg1, got)
	}
	select {
	case got := <-sent:
		t.Fatalf("unexpected extra message %q (wanted drop of %q)", got, msg2)
	case <-time.After(250 * time.Millisecond):
	}

	close(a.externalMsgs)
}

func TestEnqueueExternalMsgRoutesCriticalInterfaceToCriticalQueue(t *testing.T) {
	a := &App{
		externalMsgs:     make(chan tea.Msg, 1),
		externalCritical: make(chan tea.Msg, 1),
	}

	msg := criticalTestMsg{}
	a.enqueueExternalMsg(msg)

	if got := len(a.externalCritical); got != 1 {
		t.Fatalf("expected critical queue length 1, got %d", got)
	}
	if got := len(a.externalMsgs); got != 0 {
		t.Fatalf("expected normal queue length 0, got %d", got)
	}
}

func TestEnqueueExternalMsg_FullCriticalQueueDoesNotDropNormalQueue(t *testing.T) {
	a := &App{
		externalMsgs:     make(chan tea.Msg, 1),
		externalCritical: make(chan tea.Msg, 1),
	}

	a.externalMsgs <- testMsg("normal")
	a.externalCritical <- criticalTestMsg{}

	// Critical messages are non-evicting: enqueuing one while the critical queue
	// is full must drop the new message rather than evict the normal queue.
	if a.tryEnqueueExternalMsg(criticalTestMsg{}) {
		t.Fatal("expected enqueue to fail when critical queue is full")
	}

	if got := len(a.externalMsgs); got != 1 {
		t.Fatalf("expected normal queue length to remain 1, got %d", got)
	}
	select {
	case msg := <-a.externalMsgs:
		got, ok := msg.(testMsg)
		if !ok {
			t.Fatalf("expected normal queue message type %T, got %T", testMsg("normal"), msg)
		}
		if got != testMsg("normal") {
			t.Fatalf("expected normal queue message %q, got %q", testMsg("normal"), got)
		}
	default:
		t.Fatal("expected normal queue message to remain present")
	}
}

func readMsg(t *testing.T, ch <-chan tea.Msg) tea.Msg {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
		return nil
	}
}

func TestIsCriticalExternalMsgPinsTypes(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.Msg
		want bool
	}{
		{"messages.Error", messages.Error{Err: errors.New("x")}, true},
		{"messages.SidebarPTYStopped", messages.SidebarPTYStopped{}, true},
		{"center.PTYStopped", center.PTYStopped{}, true},
		{"marker interface impl", criticalTestMsg{}, true},
		{"plain message", testMsg("ordinary"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCriticalExternalMsg(tt.msg); got != tt.want {
				t.Fatalf("isCriticalExternalMsg(%T) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

// TestSidebarTerminalLifecycleMsgsSurviveFullQueue pins plan 128: the
// sidebar terminal create/reattach result messages are the only releasers
// of the sidebar's pendingCreation mark — under a saturated normal queue
// they must land on the critical channel, not drop.
func TestSidebarTerminalLifecycleMsgsSurviveFullQueue(t *testing.T) {
	a := &App{
		externalMsgs:     make(chan tea.Msg, 1),
		externalCritical: make(chan tea.Msg, 8),
	}
	a.externalMsgs <- testMsg("filler") // saturate the normal queue

	critical := []tea.Msg{
		sidebar.SidebarTerminalCreated{WorkspaceID: "ws"},
		sidebar.SidebarTerminalCreateFailed{WorkspaceID: "ws"},
		sidebar.SidebarTerminalReattachResult{WorkspaceID: "ws"},
		sidebar.SidebarTerminalReattachFailed{WorkspaceID: "ws"},
	}
	for _, msg := range critical {
		if !a.tryEnqueueExternalMsg(msg) {
			t.Fatalf("critical lifecycle msg %T dropped under full normal queue", msg)
		}
	}
	if got := len(a.externalCritical); got != len(critical) {
		t.Fatalf("expected %d critical msgs, got %d", len(critical), got)
	}
	if got := len(a.externalMsgs); got != 1 {
		t.Fatalf("normal queue was disturbed: len=%d", got)
	}
}
