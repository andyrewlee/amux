package app

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

type testMsg string

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
	case <-time.After(50 * time.Millisecond):
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
	case <-time.After(50 * time.Millisecond):
	}

	close(a.externalMsgs)
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
