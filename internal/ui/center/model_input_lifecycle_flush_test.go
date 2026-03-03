package center

import (
	"bytes"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestPopPTYFlushChunk_UsesLargerChunkForActiveTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	tab := &Tab{
		ID:                TabID("tab-active"),
		Workspace:         ws,
		Terminal:          vterm.New(80, 24),
		Running:           true,
		lastOutputAt:      time.Now().Add(-time.Second),
		flushPendingSince: time.Now().Add(-time.Second),
	}
	tab.pendingOutput.Append(bytes.Repeat([]byte("x"), ptyFlushChunkSizeActive+17))

	chunk, hasMore, _ := m.popPTYFlushChunk(tab, true)
	if got, want := len(chunk), ptyFlushChunkSizeActive; got != want {
		t.Fatalf("chunk size = %d, want %d", got, want)
	}
	if !hasMore {
		t.Fatal("expected remaining buffered output")
	}

	if got, want := tab.pendingOutput.Len(), 17; got != want {
		t.Fatalf("pending output = %d, want %d", got, want)
	}
}

func TestPopPTYFlushChunk_UsesBaseChunkForInactiveTab(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	inactive := &Tab{
		ID:                TabID("tab-inactive"),
		Workspace:         ws,
		Terminal:          vterm.New(80, 24),
		Running:           true,
		lastOutputAt:      time.Now().Add(-time.Second),
		flushPendingSince: time.Now().Add(-time.Second),
	}
	inactive.pendingOutput.Append(bytes.Repeat([]byte("x"), ptyFlushChunkSize+17))

	chunk, hasMore, _ := m.popPTYFlushChunk(inactive, false)
	if got, want := len(chunk), ptyFlushChunkSize; got != want {
		t.Fatalf("chunk size = %d, want %d", got, want)
	}
	if !hasMore {
		t.Fatal("expected remaining buffered output")
	}

	if got, want := inactive.pendingOutput.Len(), 17; got != want {
		t.Fatalf("pending output = %d, want %d", got, want)
	}
}
