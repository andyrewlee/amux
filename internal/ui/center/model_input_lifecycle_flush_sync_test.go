package center

import (
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/ui/ptyio"
	"github.com/andyrewlee/amux/internal/vterm"
)

const (
	testSyncBegin = "\x1b[?2026h"
	testSyncEnd   = "\x1b[?2026l"
	// ptyFlushQuietElapsed is how long output has been idle in these tests:
	// past every quiet period, short of every flush ceiling. Asserted in
	// syncFlushTab so a retuning of either constant fails loudly instead of
	// quietly turning these into tests of the quiet branch.
	ptyFlushQuietElapsed = 30 * time.Millisecond
)

// syncFlushTab builds an active tab whose quiet period has elapsed (so the
// flush gate is past its first branch) while both partial-frame ceilings are
// still far off. The elapsed window clears every quiet value flushTiming can
// pick — including the longer alt-screen one — so a deferral in these tests can
// only come from the partial-frame hold, never from the quiet period.
func syncFlushTab(t *testing.T, id, pending string) (*Model, *Tab, string) {
	t.Helper()
	if ptyFlushQuietElapsed <= ptyFlushQuiet || ptyFlushQuietElapsed <= ptyFlushQuietAlt {
		t.Fatalf("ptyFlushQuietElapsed %v must exceed both quiet periods (%v, %v)",
			ptyFlushQuietElapsed, ptyFlushQuiet, ptyFlushQuietAlt)
	}
	if ptyFlushQuietElapsed >= ptyFlushMaxInterval {
		t.Fatalf("ptyFlushQuietElapsed %v must stay under the flush ceiling %v",
			ptyFlushQuietElapsed, ptyFlushMaxInterval)
	}
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	quietElapsed := time.Now().Add(-ptyFlushQuietElapsed)
	tab := &Tab{
		ID:        TabID(id),
		Workspace: ws,
		Terminal:  vterm.New(80, 24),
		Running:   true,
		State: ptyio.State{
			LastOutputAt:      quietElapsed,
			FlushPendingSince: quietElapsed,
			PendingOutput:     []byte(pending),
		},
	}
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0
	m.workspace = ws
	return m, tab, wsID
}

// Some agents write the DEC 2026 sync-begin on its own, well ahead of the frame
// body. Flushing it alone freezes the vterm on the previous frame and marks
// every line dirty until the region closes, so the flush must wait instead.
func TestUpdatePTYFlush_HoldsLoneSyncBegin(t *testing.T) {
	m, tab, wsID := syncFlushTab(t, "tab-sync-begin", testSyncBegin)

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})

	if got, want := string(tab.PendingOutput), testSyncBegin; got != want {
		t.Fatalf("pending output = %q, want the sync-begin still buffered", got)
	}
	if tab.Terminal.SyncActive() {
		t.Fatal("terminal is sync-frozen, want the partial frame withheld")
	}
}

// Once the body closes the region the whole frame flushes in one write, leaving
// the terminal unfrozen and the content visible.
func TestUpdatePTYFlush_WritesCompletedSyncFrame(t *testing.T) {
	m, tab, wsID := syncFlushTab(t, "tab-sync-frame", testSyncBegin+"hello"+testSyncEnd)

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})

	if len(tab.PendingOutput) != 0 {
		t.Fatalf("pending output = %q, want drained", tab.PendingOutput)
	}
	if tab.Terminal.SyncActive() {
		t.Fatal("terminal is sync-frozen after a complete frame")
	}
	if !strings.Contains(tab.Terminal.Render(), "hello") {
		t.Fatal("completed frame did not reach the terminal")
	}
}

// A completed frame ahead of a freshly opened one must not wait for the second
// frame: the flush splits at the boundary so the ready frame is displayed now.
func TestUpdatePTYFlush_SplitsAtCompletedSyncFrameBoundary(t *testing.T) {
	m, tab, wsID := syncFlushTab(t, "tab-sync-split", testSyncBegin+"hello"+testSyncEnd+testSyncBegin)

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})

	if got, want := string(tab.PendingOutput), testSyncBegin; got != want {
		t.Fatalf("pending output = %q, want only the next frame's sync-begin buffered", got)
	}
	if tab.Terminal.SyncActive() {
		t.Fatal("terminal is sync-frozen, want the partial second frame withheld")
	}
	if !strings.Contains(tab.Terminal.Render(), "hello") {
		t.Fatal("completed frame did not reach the terminal")
	}
}

// The hold is bounded: a writer that opens a region and goes silent must not
// stall the pane past the flush ceiling.
func TestUpdatePTYFlush_FlushesLoneSyncBeginPastCeiling(t *testing.T) {
	m, tab, wsID := syncFlushTab(t, "tab-sync-stall", testSyncBegin)
	stale := time.Now().Add(-(ptyFlushMaxInterval + 10*time.Millisecond))
	tab.State.LastOutputAt = stale
	tab.State.FlushPendingSince = stale

	_ = m.updatePTYFlush(PTYFlush{WorkspaceID: wsID, TabID: tab.ID})

	if len(tab.PendingOutput) != 0 {
		t.Fatalf("pending output = %q, want flushed once the ceiling elapsed", tab.PendingOutput)
	}
}
