package vterm

import "testing"

// A pane running in the alternate screen keeps no local scrollback: the
// application owns its viewport. The pane's screen state is the host-managed
// probe flag, not the client-stream 1049h (which tmux holds for the whole
// attach and so says nothing about the pane).
func TestAltScreenScrollbackDisabled(t *testing.T) {
	t.Parallel()
	vt := New(3, 2)
	vt.SetPaneAlternateScreen(true)

	vt.Write([]byte("a\nb\nc\n"))
	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected no scrollback on an alt-screen pane, got %d", len(vt.Scrollback))
	}
}
