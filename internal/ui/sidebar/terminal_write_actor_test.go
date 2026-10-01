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

// TestSidebarWriteActorStopsOnDetach proves teardown closes the queue and a
// post-detach enqueue creates a fresh queue rather than panicking on the
// closed one (a pending flush tick can legitimately arrive after detach).
func TestSidebarWriteActorStopsOnDetach(t *testing.T) {
	m := NewTerminalModel()
	m.SetMsgSink(func(tea.Msg) {})
	ts := &TerminalState{VTerm: vterm.New(40, 10), Running: true}
	ts.PendingOutput = append(ts.PendingOutput, 'x')
	ts.LastOutputAt = time.Now().Add(-time.Second)
	m.tabs.ByWorkspace["ws"] = []*TerminalTab{{ID: "t", State: ts}}

	if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte("z")}) {
		t.Fatal("enqueue fell back")
	}
	m.detachState(ts, false)
	ts.mu.Lock()
	q := ts.writeQ
	ts.mu.Unlock()
	if q != nil {
		t.Fatal("writeQ still set after detach")
	}
	m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte("late")})
}
