package sidebar

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
	"github.com/andyrewlee/amux/internal/vterm"
)

// sidebarTranscript renders scrollback+screen as text — the byte-order proof
// for drain tests whose markers scroll off the small test screen.
func sidebarTranscript(ts *TerminalState) string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	term := ts.VTerm
	if term == nil {
		return ""
	}
	screen, scrollbackLen := term.RenderBuffers()
	total := scrollbackLen + len(screen)
	if total == 0 {
		return ""
	}
	width := term.Width
	if width < 1 {
		width = 1
	}
	return term.GetTextRange(0, 0, width-1, total-1)
}

// assertTranscriptOrder fails unless every marker appears in transcript in
// listed order.
func assertTranscriptOrder(t *testing.T, transcript string, markers []string) {
	t.Helper()
	prev := -1
	for _, marker := range markers {
		idx := strings.Index(transcript, marker)
		if idx < 0 {
			t.Fatalf("%q missing from transcript:\n%s", marker, transcript)
		}
		if idx <= prev {
			t.Fatalf("%q out of order in transcript:\n%s", marker, transcript)
		}
		prev = idx
	}
}

// TestSidebarStopDrain_OrderAndCompleteness fills the writer queue and a
// multi-chunk PendingOutput backlog, then drives SidebarPTYStopped: every
// queued byte lands before every buffered byte, the whole backlog applies
// (split across per-request and per-chunk holds), and the buffers are left
// empty.
func TestSidebarStopDrain_OrderAndCompleteness(t *testing.T) {
	m := NewTerminalModel()
	m.SetMsgSink(func(tea.Msg) {})
	wsID := "ws-stop-drain"
	tabID := TerminalTabID("term-tab-stop-drain")
	ts := liveTerminalState()
	ts.VTerm = vterm.New(60, 20)
	ts.State.ReaderGen = 1
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: ts}}
	hook, entered, release := pauseOnceHook()
	installTestWriter(t, m, ts, hook)

	// Two stream writes queue while the worker is parked before its first
	// lock — the drain must apply them before any buffered chunk.
	if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte("queued-A\r\n"), workspaceID: wsID, tabID: string(tabID)}) {
		t.Fatal("first queued write fell back")
	}
	<-entered
	if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte("queued-B\r\n"), workspaceID: wsID, tabID: string(tabID)}) {
		t.Fatal("second queued write fell back")
	}

	// Backlog spanning several TakeFlushChunkLocked chunks — each must apply
	// under its own hold, in order, after the queued writes.
	ts.mu.Lock()
	ts.PendingOutput = append(ts.PendingOutput, []byte("buf-head\r\n")...)
	ts.PendingOutput = append(ts.PendingOutput, bytes.Repeat([]byte{'x'}, ptyio.PtyFlushChunkSize)...)
	ts.PendingOutput = append(ts.PendingOutput, []byte("\r\nbuf-mid\r\n")...)
	ts.PendingOutput = append(ts.PendingOutput, bytes.Repeat([]byte{'x'}, ptyio.PtyFlushChunkSize)...)
	ts.PendingOutput = append(ts.PendingOutput, []byte("\r\nbuf-tail\r\n")...)
	ts.pendingBufferedBytes = len(ts.PendingOutput)
	ts.mu.Unlock()

	m.handlePTYStopped(messages.SidebarPTYStopped{WorkspaceID: wsID, TabID: string(tabID), Gen: 1})
	close(release)

	assertTranscriptOrder(t, sidebarTranscript(ts), []string{"queued-A", "queued-B", "buf-head", "buf-mid", "buf-tail"})

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if len(ts.PendingOutput) != 0 {
		t.Fatalf("stop drain left %d pending bytes", len(ts.PendingOutput))
	}
	if ts.pendingBufferedBytes != 0 {
		t.Fatalf("pendingBufferedBytes = %d, want 0 after drain", ts.pendingBufferedBytes)
	}
	if ts.writer != nil && len(ts.writer.reqs) != 0 {
		t.Fatalf("writer queue still holds %d requests", len(ts.writer.reqs))
	}
}

// TestSidebarFlush_QueueFullFallbackPreservesOrder drives handlePTYFlush with
// a full writer queue: the rejected chunk must apply after every queued
// request (byte order across the bounded holds), and the stream keeps working
// afterwards — the fallback drained the queue rather than dropping it.
func TestSidebarFlush_QueueFullFallbackPreservesOrder(t *testing.T) {
	m := NewTerminalModel()
	m.SetMsgSink(func(tea.Msg) {})
	wsID := "ws-queue-full"
	tabID := TerminalTabID("term-tab-queue-full")
	ts := &TerminalState{VTerm: vterm.New(60, 20), Running: true}
	m.tabs.ByWorkspace[wsID] = []*TerminalTab{{ID: tabID, State: ts}}
	m.tabs.ActiveByWorkspace[wsID] = 0
	hook, entered, release := pauseOnceHook()
	installTestWriter(t, m, ts, hook)

	if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte("seed\r\n"), workspaceID: wsID, tabID: string(tabID)}) {
		t.Fatal("seed fell back")
	}
	<-entered
	ts.mu.Lock()
	enqueued := 1
	for i := 0; i < sidebarWriteQueueCap*2; i++ {
		req := sidebarWriteReq{chunk: []byte{'q', byte('0' + i), '\r', '\n'}, workspaceID: wsID, tabID: string(tabID)}
		if m.enqueueSidebarWriteLocked(ts, req) {
			enqueued++
		}
	}
	if enqueued != sidebarWriteQueueCap {
		ts.mu.Unlock()
		t.Fatalf("queue accepted %d requests, want exactly cap %d", enqueued, sidebarWriteQueueCap)
	}
	// One pending chunk the flush tick will try to enqueue — the queue is
	// full, so it becomes the fallback's failed request.
	ts.PendingOutput = []byte("pending-tail\r\n")
	ts.pendingBufferedBytes = len(ts.PendingOutput)
	ts.LastOutputAt = time.Now().Add(-time.Second)
	ts.mu.Unlock()

	m.handlePTYFlush(messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: string(tabID)})

	want := []string{"seed", "q0", "q1", "q2", "q3", "q4", "q5", "q6", "pending-tail"}
	assertTranscriptOrder(t, sidebarTranscript(ts), want)

	// Functional completeness: the queue drained, so post-fallback writes
	// flow through the worker again.
	close(release)
	if !m.enqueueSidebarWrite(ts, sidebarWriteReq{chunk: []byte("after-fallback\r\n"), workspaceID: wsID, tabID: string(tabID)}) {
		t.Fatal("post-fallback enqueue rejected — queue never drained")
	}
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(sidebarTranscript(ts), "after-fallback") {
		if time.Now().After(deadline) {
			t.Fatal("post-fallback write never applied")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
