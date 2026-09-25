package center

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/vterm"
)

// enqueueWriteEvent builds a write-output tabEvent the way
// dispatchFlushChunkViaActor does: enqueueActorWrite runs the noise-filter
// preview, so the event carries the filtered bytes and the post-filter noise
// trailing the apply commits. Tests that hand-assemble the event without these
// fields would apply zero bytes — the apply no longer re-filters.
func enqueueWriteEvent(t *testing.T, tab *Tab, chunk []byte) tabEvent {
	t.Helper()
	prevEpoch, _, _, filtered, noiseAfter := enqueueActorWrite(tab, chunk)
	return tabEvent{
		tab:            tab,
		kind:           tabEventWriteOutput,
		output:         chunk,
		filteredOutput: filtered,
		noiseAfter:     noiseAfter,
		writeEpoch:     prevEpoch,
	}
}

// TestActorWriteCarriesPreviewThroughEvent pins the single-filter contract:
// enqueueActorWrite's preview produces the bytes the actor applies and the
// noise trailing it commits, so applyActorWriteLocked never re-filters. A
// diagnostic fragment split across two chunks must resolve identically through
// the preview→commit chain as it would through a live re-filter.
func TestActorWriteCarriesPreviewThroughEvent(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	term := vterm.New(80, 24)
	tab := &Tab{
		ID:        TabID("tab-preview-carry"),
		Assistant: "codex",
		Workspace: ws,
		Terminal:  term,
		Running:   true,
	}
	m.AddTab(tab)

	// Chunk 1 ends mid-diagnostic: "proc(12" is a potential PID prefix the
	// filter holds back as trailing. Chunk 2 completes the diagnostic line,
	// which must be filtered out entirely.
	chunk1 := []byte("visible one\nproc(12")
	ev1 := enqueueWriteEvent(t, tab, chunk1)
	m.handleTabEvent(ev1)
	if got := tab.NoiseTrailing; string(got) != "proc(12" {
		t.Fatalf("expected committed noise trailing %q after first write, got %q", "proc(12", got)
	}

	chunk2 := []byte("3) malloc: ** error\nvisible two\n")
	ev2 := enqueueWriteEvent(t, tab, chunk2)
	m.handleTabEvent(ev2)
	if len(tab.NoiseTrailing) != 0 {
		t.Fatalf("expected noise trailing drained after diagnostic completed, got %q", tab.NoiseTrailing)
	}

	// The terminal must contain only the visible lines — the split diagnostic
	// is removed end-to-end through the carried previews.
	screen := term.Render()
	if strings.Contains(screen, "malloc") || strings.Contains(screen, "proc(") {
		t.Fatalf("expected diagnostic line filtered from terminal, screen contains it: %q", screen)
	}
	if !strings.Contains(screen, "visible one") || !strings.Contains(screen, "visible two") {
		t.Fatalf("expected visible lines in terminal, got %q", screen)
	}
}

// TestActorWriteDrainsPendingBell pins the parser→app handoff: a BEL inside an
// output chunk surfaces as a TabBell message (the attention edge), and a
// bell-free write emits none.
func TestActorWriteDrainsPendingBell(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	term := vterm.New(80, 24)
	tab := &Tab{
		ID:        TabID("tab-bell"),
		Assistant: "claude",
		Workspace: ws,
		Terminal:  term,
		Running:   true,
	}
	m.AddTab(tab)

	sinkMsgs := make(chan tea.Msg, 8)
	m.msgSink = func(msg tea.Msg) { sinkMsgs <- msg }

	ev := enqueueWriteEvent(t, tab, []byte("working...\x07done?"))
	m.handleTabEvent(ev)

	var bells int
	drain := func() {
		for {
			select {
			case msg := <-sinkMsgs:
				if _, ok := msg.(TabBell); ok {
					bells++
				}
			default:
				return
			}
		}
	}
	drain()
	if bells != 1 {
		t.Fatalf("expected exactly one TabBell from a belled write, got %d", bells)
	}

	// A plain write emits no bell.
	ev2 := enqueueWriteEvent(t, tab, []byte("quiet output\n"))
	m.handleTabEvent(ev2)
	drain()
	if bells != 1 {
		t.Fatalf("bell-free write emitted a TabBell (total %d)", bells)
	}
}
