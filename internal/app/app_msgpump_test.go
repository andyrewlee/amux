package app

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
)

type testMsg string

type criticalTestMsg struct{}

func (criticalTestMsg) MarkCriticalExternalMsg() {}

var _ common.CriticalExternalMsg = criticalTestMsg{}

type nonEvictingCriticalTestMsg struct{}

func (nonEvictingCriticalTestMsg) MarkCriticalExternalMsg()            {}
func (nonEvictingCriticalTestMsg) MarkNonEvictingCriticalExternalMsg() {}

var (
	_ common.CriticalExternalMsg            = nonEvictingCriticalTestMsg{}
	_ common.NonEvictingCriticalExternalMsg = nonEvictingCriticalTestMsg{}
)

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

func TestNonEvictingCriticalInterfaceImpliesCriticalRouting(t *testing.T) {
	var msg any = nonEvictingCriticalTestMsg{}
	if _, ok := msg.(common.NonEvictingCriticalExternalMsg); !ok {
		t.Fatal("expected test message to implement NonEvictingCriticalExternalMsg")
	}
	if _, ok := msg.(common.CriticalExternalMsg); !ok {
		t.Fatal("expected NonEvictingCriticalExternalMsg to imply CriticalExternalMsg")
	}
}

func TestEnqueueExternalMsg_NonEvictingCriticalDoesNotDropNormalQueue(t *testing.T) {
	a := &App{
		externalMsgs:     make(chan tea.Msg, 1),
		externalCritical: make(chan tea.Msg, 1),
	}

	a.externalMsgs <- testMsg("normal")
	a.externalCritical <- criticalTestMsg{}

	a.enqueueExternalMsg(nonEvictingCriticalTestMsg{})

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
