package common

import (
	"strconv"
	"testing"
	"time"
)

func TestEventRingPushSnapshotOrdering(t *testing.T) {
	var r EventRing
	if r.Len() != 0 {
		t.Fatalf("new ring Len = %d, want 0", r.Len())
	}
	base := time.Now()
	for i := 0; i < 3; i++ {
		r.Push(SessionEvent{Time: base.Add(time.Duration(i) * time.Second), Type: ToastInfo, Text: "e" + strconv.Itoa(i)})
	}
	got := r.Snapshot()
	if len(got) != 3 {
		t.Fatalf("Snapshot len = %d, want 3", len(got))
	}
	// Newest-first: e2, e1, e0.
	for i, want := range []string{"e2", "e1", "e0"} {
		if got[i].Text != want {
			t.Fatalf("Snapshot[%d] = %q, want %q", i, got[i].Text, want)
		}
	}
	if got[0].Type != ToastInfo {
		t.Fatalf("Snapshot[0].Type = %v, want ToastInfo", got[0].Type)
	}
}

func TestEventRingCapacityWrap(t *testing.T) {
	var r EventRing
	for i := 0; i < EventRingCapacity+10; i++ {
		r.Push(SessionEvent{Time: time.Now(), Type: ToastInfo, Text: strconv.Itoa(i)})
	}
	if r.Len() != EventRingCapacity {
		t.Fatalf("Len = %d, want %d", r.Len(), EventRingCapacity)
	}
	got := r.Snapshot()
	if got[0].Text != strconv.Itoa(EventRingCapacity+10-1) {
		t.Fatalf("newest = %q, want %d", got[0].Text, EventRingCapacity+10-1)
	}
	if got[len(got)-1].Text != "10" {
		t.Fatalf("oldest retained = %q, want 10 (first 10 dropped)", got[len(got)-1].Text)
	}
}

func TestEventRingSnapshotIsCopy(t *testing.T) {
	var r EventRing
	r.Push(SessionEvent{Time: time.Now(), Type: ToastInfo, Text: "orig"})
	got := r.Snapshot()
	got[0].Text = "mutated"
	if r.Snapshot()[0].Text != "orig" {
		t.Fatal("mutating the snapshot leaked into the ring")
	}
}

func TestToastModelShowCapturesEvents(t *testing.T) {
	m := NewToastModel()
	m.ShowInfo("first")
	m.ShowError("second")
	m.ShowWarning("third")
	m.Show("fourth", ToastSuccess, time.Second)

	events := m.Events()
	if len(events) != 4 {
		t.Fatalf("Events len = %d, want 4", len(events))
	}
	// Newest-first, with severities preserved through the funnel.
	want := []struct {
		text string
		typ  ToastType
	}{
		{"fourth", ToastSuccess},
		{"third", ToastWarning},
		{"second", ToastError},
		{"first", ToastInfo},
	}
	for i, w := range want {
		if events[i].Text != w.text || events[i].Type != w.typ {
			t.Fatalf("Events[%d] = {%q %v}, want {%q %v}", i, events[i].Text, events[i].Type, w.text, w.typ)
		}
	}
	if events[0].Time.IsZero() {
		t.Fatal("event time was not stamped")
	}
}
