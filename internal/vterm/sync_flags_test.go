package vterm

import (
	"bytes"
	"fmt"
	"testing"
)

// TestScrollDuringSyncTogglesPreserveViewport covers the sync-mode viewport
// anchor flag: scrolling up during a synchronized-output window anchors the
// frozen viewport, and returning to offset 0 releases the anchor so growth
// recorded during the sync is not applied at sync end.
func TestScrollDuringSyncTogglesPreserveViewport(t *testing.T) {
	t.Parallel()
	v := New(20, 5)
	for i := 0; i < 30; i++ {
		v.Write([]byte(fmt.Sprintf("line %d\r\n", i)))
	}

	v.Write([]byte("\x1b[?2026h")) // sync start while at the live view
	if !v.syncActive {
		t.Fatal("expected sync mode active")
	}
	if v.syncPreserveViewport {
		t.Fatal("preserve flag must start false when sync begins at offset 0")
	}

	v.ScrollView(3)
	if !v.syncPreserveViewport {
		t.Fatal("scrolling up during sync must anchor the viewport")
	}

	v.ScrollViewToBottom()
	if v.syncPreserveViewport {
		t.Fatal("scrolling back to offset 0 during sync must release the anchor")
	}

	// Hidden scrollback growth recorded during the sync must be discarded at
	// sync end once the user has returned to the live view.
	v.syncViewOffsetDelta = 7
	v.Write([]byte("\x1b[?2026l"))
	if v.ViewOffset != 0 {
		t.Fatalf("expected live view after sync end, got ViewOffset=%d", v.ViewOffset)
	}
	if v.syncPreserveViewport || v.syncViewOffsetDelta != 0 {
		t.Fatalf("expected sync viewport state cleared, got preserve=%v delta=%d",
			v.syncPreserveViewport, v.syncViewOffsetDelta)
	}
}

// TestSyncEndEnforcesDeferredScrollbackTrim covers the deferred-trim flag:
// scrollback growth past MaxScrollback during a synchronized-output window is
// deferred (the frozen snapshot must stay mappable) and enforced as soon as
// the sync ends, no matter how much the sync overflowed.
func TestSyncEndEnforcesDeferredScrollbackTrim(t *testing.T) {
	t.Parallel()
	v := New(20, 5)

	var buf bytes.Buffer
	for i := 0; i < MaxScrollback; i++ {
		fmt.Fprintf(&buf, "pre %d\r\n", i)
	}
	v.Write(buf.Bytes())

	v.Write([]byte("\x1b[?2026h"))

	buf.Reset()
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&buf, "sync %d\r\n", i)
	}
	v.Write(buf.Bytes())

	if !v.syncDeferTrim {
		t.Fatal("expected trim deferred while sync is active")
	}
	if len(v.Scrollback) <= MaxScrollback {
		t.Fatalf("precondition: scrollback should exceed cap during sync, got %d", len(v.Scrollback))
	}

	v.Write([]byte("\x1b[?2026l"))
	if v.syncDeferTrim {
		t.Fatal("expected deferred-trim flag cleared at sync end")
	}
	if len(v.Scrollback) != MaxScrollback {
		t.Fatalf("expected scrollback trimmed to %d at sync end, got %d", MaxScrollback, len(v.Scrollback))
	}
}
