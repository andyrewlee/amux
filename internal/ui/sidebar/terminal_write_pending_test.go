package sidebar

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/vterm"
)

// Publication and stream-end coverage for the writer's pending-byte contract —
// the buffer-tracking subtests of TestSidebarWriterPendingPublication live here
// to keep terminal_write_integrity_test.go under the file-length cap.

func TestSidebarWriterPendingBufferTracking(t *testing.T) {
	t.Run("published count tracks append, flush take, and overflow", func(t *testing.T) {
		m := NewTerminalModel()
		m.SetMsgSink(func(tea.Msg) {})
		m.width = 60
		m.height = 20
		wsID := "ws-pub"
		tabID := TerminalTabID("t")
		ts := &TerminalState{VTerm: vterm.New(60, 20), Running: true}
		m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: ts}}
		m.tabs.ActiveByWorkspace[wsID] = 0

		// Append publishes the exact buffered length once AppendOutput ends.
		m.handlePTYOutput(messages.SidebarPTYOutput{WorkspaceID: wsID, TabID: string(tabID), Data: []byte("hello")})
		ts.mu.Lock()
		if ts.pendingBufferedBytes != len(ts.PendingOutput) {
			t.Fatalf("post-append published %d, want %d", ts.pendingBufferedBytes, len(ts.PendingOutput))
		}
		ts.LastOutputAt = time.Now().Add(-time.Second) // bypass the flush gate
		ts.mu.Unlock()

		// The flush take shrinks the buffer — the republished count tracks it.
		m.handlePTYFlush(messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: string(tabID)})
		ts.mu.Lock()
		if ts.pendingBufferedBytes != len(ts.PendingOutput) {
			t.Fatalf("post-flush published %d, want %d", ts.pendingBufferedBytes, len(ts.PendingOutput))
		}
		ts.mu.Unlock()

		// Overflow trims the buffer — the republished count follows the
		// retained length, not the pre-append reservation.
		big := make([]byte, ptyMaxBufferedBytes+512)
		for i := range big {
			big[i] = 'a' + byte(i%26)
		}
		m.handlePTYOutput(messages.SidebarPTYOutput{WorkspaceID: wsID, TabID: string(tabID), Data: big})
		ts.mu.Lock()
		if ts.pendingBufferedBytes != len(ts.PendingOutput) {
			t.Fatalf("post-overflow published %d, want %d", ts.pendingBufferedBytes, len(ts.PendingOutput))
		}
		if len(ts.PendingOutput) > ptyMaxBufferedBytes {
			t.Fatalf("buffer kept %d bytes over the cap", len(ts.PendingOutput))
		}
		ts.mu.Unlock()

		// A writer stream installed by the flush drain (the flush fell back
		// or enqueued — either way a worker may exist) is stopped so the test
		// leaves no goroutine.
		ts.mu.Lock()
		retired := ts.stopSidebarWriterLocked()
		ts.mu.Unlock()
		joinSidebarWriter(retired)
	})

	t.Run("stream end drains queue, buffer, then the noise tail", func(t *testing.T) {
		m := NewTerminalModel()
		m.SetMsgSink(func(tea.Msg) {})
		wsID := "ws-end"
		tabID := TerminalTabID("t")
		ts := &TerminalState{VTerm: vterm.New(60, 20), Running: true}
		m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: ts}}
		m.tabs.ActiveByWorkspace[wsID] = 0
		installTestWriter(t, m, ts, nil)

		// Queue one stream request, then buffer more output behind it and
		// leave a noise-looking tail held from an earlier apply.
		if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte("queued\n"), workspaceID: wsID, tabID: string(tabID)}) {
			t.Fatal("queued chunk fell back")
		}
		ts.mu.Lock()
		ts.PendingOutput = append(ts.PendingOutput, []byte("buffered\n")...)
		ts.PendingOutput = append(ts.PendingOutput, []byte("tail\nagent(7) malloc: frag")...)
		ts.pendingBufferedBytes = len(ts.PendingOutput)
		ts.mu.Unlock()

		m.handlePTYStopped(messages.SidebarPTYStopped{WorkspaceID: wsID, TabID: string(tabID)})

		screen := renderSidebarScreen(ts)
		qIdx := strings.Index(screen, "queued")
		bIdx := strings.Index(screen, "buffered")
		tIdx := strings.Index(screen, "tail")
		if qIdx < 0 || bIdx < 0 || tIdx < 0 {
			t.Fatalf("stream-end drain lost bytes: screen %q", screen)
		}
		if !(qIdx < bIdx && bIdx < tIdx) {
			t.Fatalf("stream-end order wrong: queued=%d buffered=%d tail=%d screen %q", qIdx, bIdx, tIdx, screen)
		}
		ts.mu.Lock()
		if ts.pendingBufferedBytes != len(ts.PendingOutput) {
			t.Fatalf("post-stop published %d, want %d", ts.pendingBufferedBytes, len(ts.PendingOutput))
		}
		ts.mu.Unlock()
	})
}
