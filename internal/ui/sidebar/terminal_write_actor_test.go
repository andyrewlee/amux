package sidebar

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/vterm"
)

// collectWritten returns a msgSink that forwards each SidebarTabWritten to
// got (may be nil).
func collectWritten(got chan<- SidebarTabWritten) func(tea.Msg) {
	return func(msg tea.Msg) {
		if res, ok := msg.(SidebarTabWritten); ok && got != nil {
			got <- res
		}
	}
}

// TestSidebarWriteActorOrdersChunks proves queued writes reach the VTerm in
// submission order: the worker serializes applySidebarWriteLocked.
func TestSidebarWriteActorOrdersChunks(t *testing.T) {
	m := NewTerminalModel()
	results := make(chan SidebarTabWritten, 64)
	m.SetMsgSink(collectWritten(results))

	ts := &TerminalState{VTerm: vterm.New(40, 10), Running: true}
	ts.PendingOutput = append(ts.PendingOutput, 'x')
	ts.LastOutputAt = time.Now().Add(-time.Second)

	const n = 8
	for i := 0; i < n; i++ {
		if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte(fmt.Sprintf("w%d\r\n", i))}) {
			t.Fatalf("enqueue %d unexpectedly fell back", i)
		}
	}
	for i := 0; i < n; i++ {
		select {
		case <-results:
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for write result %d", i)
		}
	}
	var screen string
	testutil.Eventually(t, 2*time.Second, 10*time.Millisecond, func() bool {
		ts.mu.Lock()
		screen = ts.VTerm.Render()
		ts.mu.Unlock()
		return strings.Contains(screen, fmt.Sprintf("w%d", n-1))
	}, "chunk %d never reached the vterm", n-1)
	prev := -1
	for i := 0; i < n; i++ {
		idx := strings.Index(screen, fmt.Sprintf("w%d", i))
		if idx < 0 {
			t.Fatalf("chunk %d missing from screen %q", i, screen)
		}
		if idx <= prev {
			t.Fatalf("writes out of order at %d: screen %q", i, screen)
		}
		prev = idx
	}
}

// TestSidebarWriteActorStopsOnDetach proves teardown retires the stream and
// joins its worker: the old stream's done closes, its epoch is fenced, and a
// post-detach enqueue installs a fresh stream rather than reviving the old
// one (a pending flush tick can legitimately arrive after detach — that is
// current-stream work, not a stale old-worker request).
func TestSidebarWriteActorStopsOnDetach(t *testing.T) {
	m := NewTerminalModel()
	results := make(chan SidebarTabWritten, 8)
	m.SetMsgSink(collectWritten(results))
	ts := &TerminalState{VTerm: vterm.New(40, 10), Running: true}
	ts.PendingOutput = append(ts.PendingOutput, 'x')
	ts.LastOutputAt = time.Now().Add(-time.Second)
	m.tabs.ByWorkspace["ws"] = []*TerminalTab{{ID: "t", State: ts}}

	if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte("z"), workspaceID: "ws", tabID: "t"}) {
		t.Fatal("enqueue fell back")
	}
	ts.mu.Lock()
	retired := ts.writer
	epochBefore := ts.writeEpoch
	ts.mu.Unlock()

	m.detachState(ts, false)

	// The retired stream's worker was joined by detach: done is closed and
	// the tab no longer points at it.
	select {
	case <-retired.done:
	case <-time.After(3 * time.Second):
		t.Fatal("detach did not join the retired writer")
	}
	ts.mu.Lock()
	if ts.writer != nil {
		t.Fatal("writer stream still installed after detach")
	}
	if ts.writeEpoch <= epochBefore {
		t.Fatal("detach did not fence the old stream's epoch")
	}
	ts.mu.Unlock()

	// A late flush for preserved detached history installs a NEW stream —
	// its completion carries the new epoch, not the retired one.
	if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte("late"), workspaceID: "ws", tabID: "t"}) {
		t.Fatal("post-detach enqueue fell back")
	}
	ts.mu.Lock()
	fresh := ts.writer
	ts.mu.Unlock()
	if fresh == retired {
		t.Fatal("post-detach enqueue revived the retired stream")
	}
	// The seed's completion may still be in flight from the retired stream
	// (stamped with the old epoch) — skip stale results until the fresh
	// stream's own completion arrives.
	sawFresh := false
	for deadline := time.Now().Add(3 * time.Second); !sawFresh && time.Now().Before(deadline); {
		select {
		case res := <-results:
			sawFresh = res.epoch == fresh.epoch
		case <-time.After(3 * time.Second):
		}
	}
	if !sawFresh {
		t.Fatal("timed out waiting for the fresh stream's completion")
	}
	ts.mu.Lock()
	ts.stopSidebarWriterLocked()
	ts.mu.Unlock()
	joinSidebarWriter(fresh)
}
