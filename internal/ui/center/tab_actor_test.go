package center

import "testing"

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
