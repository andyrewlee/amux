package sidebar

import (
	"fmt"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

// installSidebarSnapshotSeams wires an eligible session: the pre-attach resize
// and full-pane capture both succeed, and the post-attach probe reports the
// single attaching client so the snapshot survives validation. Resize and attach
// calls record their dimensions so a test can pin what size each ran at.
func installSidebarSnapshotSeams(t *testing.T, calls *[]string, snapshotData string) {
	t.Helper()
	restoreAttachSeams(t)

	attached := eligibleAttachProbe()
	attached.ClientCount = 1
	// The pane ends up at the size the bootstrap resized it to, which is what the
	// snapshot metadata must report.
	sized := func(p tmux.SessionProbe) tmux.SessionProbe {
		p.PaneCols, p.PaneRows = 77, 19
		p.PaneMeta.Cols, p.PaneMeta.Rows = 77, 19
		return p
	}

	ensureTmuxAvailableFn = func() error { return nil }
	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		*calls = append(*calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	probeSeq(calls,
		eligibleAttachProbe(),
		sized(eligibleAttachProbe()),
		sized(eligibleAttachProbe()),
		sized(attached),
	)
	resizePaneToSizeFn = func(_ string, cols, rows int, _ tmux.Options) error {
		*calls = append(*calls, fmt.Sprintf("resize:%dx%d", cols, rows))
		return nil
	}
	capturePaneFullDataFn = func(string, tmux.Options) ([]byte, error) {
		*calls = append(*calls, "snapshot")
		return []byte(snapshotData), nil
	}
	capturePaneHistoryDataFn = func(string, tmux.Options) ([]byte, error) {
		*calls = append(*calls, "scrollback")
		return []byte("fallback"), nil
	}
	capturePaneFn = func(string, tmux.Options) ([]byte, error) {
		*calls = append(*calls, "scrollback")
		return []byte("fallback"), nil
	}
	newPTYWithSizeFn = func(command, dir string, env []string, rows, cols uint16) (*pty.Terminal, error) {
		*calls = append(*calls, fmt.Sprintf("attach:%dx%d", cols, rows))
		return &pty.Terminal{}, nil
	}
	verifyTerminalSessionTagsFn = func(sessionName string, tags tmux.SessionTags, opts tmux.Options) error {
		*calls = append(*calls, "verify")
		return nil
	}
}

// assertSidebarSnapshotOrder pins the ordering the pre-attach snapshot depends
// on: probe, then resize to the client's size, then snapshot, then attach, then
// exactly one reconciliation history capture.
func assertSidebarSnapshotOrder(t *testing.T, calls []string, termWidth, termHeight int) {
	t.Helper()
	expectedAttach := fmt.Sprintf("attach:%dx%d", termWidth, termHeight)
	expectedResize := fmt.Sprintf("resize:%dx%d", termWidth, termHeight)

	probeIdx, resizeIdx, snapshotIdx, attachIdx := -1, -1, -1, -1
	scrollbackIdx, verifyIdx := -1, -1
	snapshotCount, scrollbackCount := 0, 0
	for i, call := range calls {
		switch call {
		case "probe":
			if probeIdx == -1 {
				probeIdx = i
			}
		case expectedResize:
			if resizeIdx == -1 {
				resizeIdx = i
			}
		case "snapshot":
			snapshotCount++
			if snapshotIdx == -1 {
				snapshotIdx = i
			}
		case expectedAttach:
			attachIdx = i
		case "scrollback":
			scrollbackCount++
			if scrollbackIdx == -1 {
				scrollbackIdx = i
			}
		case "verify":
			verifyIdx = i
		}
	}

	if probeIdx == -1 {
		t.Fatalf("expected a bootstrap probe before the resize, got %v", calls)
	}
	if resizeIdx == -1 {
		t.Fatalf("expected resize call %q, got %v", expectedResize, calls)
	}
	if attachIdx == -1 {
		t.Fatalf("expected attach call %q, got %v", expectedAttach, calls)
	}
	if snapshotCount != 1 {
		t.Fatalf("expected a single resized snapshot capture, got %v", calls)
	}
	if probeIdx > resizeIdx || resizeIdx > snapshotIdx {
		t.Fatalf("expected probe before resize and snapshot after resize, got call order %v", calls)
	}
	if snapshotIdx > attachIdx {
		t.Fatalf("expected the snapshot before the live attach, got call order %v", calls)
	}
	if scrollbackCount != 1 {
		t.Fatalf("expected a single post-attach delta capture, got %v", calls)
	}
	if scrollbackIdx < attachIdx {
		t.Fatalf("expected reconciliation history capture after attach, got %v", calls)
	}
	if verifyIdx != -1 && scrollbackIdx > verifyIdx {
		t.Fatalf("expected history reconciliation before session tag verification, got %v", calls)
	}
}

func TestCreateTerminalTab_CapturesReusedSessionBeforeAttachAfterResize(t *testing.T) {
	var calls []string
	installSidebarSnapshotSeams(t, &calls, "resized frame")

	m := NewTerminalModel()
	m.width = 20
	m.height = 5
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	termWidth, termHeight := m.terminalContentSize()

	msg := m.createTerminalTab(ws)()
	created, ok := msg.(SidebarTerminalCreated)
	if !ok {
		t.Fatalf("expected SidebarTerminalCreated, got %T", msg)
	}
	if !created.CaptureFullPane {
		t.Fatal("expected reused session startup to restore a full-pane snapshot")
	}
	if got := string(created.ScrollbackCapture); got != "resized frame" {
		t.Fatalf("expected snapshot data from pre-attach capture, got %q", got)
	}
	if got := string(created.PostAttachScrollbackCapture); got != "fallback" {
		t.Fatalf("expected post-attach history reconciliation capture, got %q", got)
	}
	if created.SnapshotCols != 77 || created.SnapshotRows != 19 {
		t.Fatalf("expected actual snapshot size 77x19, got %dx%d", created.SnapshotCols, created.SnapshotRows)
	}
	assertSidebarSnapshotOrder(t, calls, termWidth, termHeight)
}

func TestAttachToSession_CapturesReattachSnapshotBeforeAttach(t *testing.T) {
	var calls []string
	installSidebarSnapshotSeams(t, &calls, "pre-attach frame")

	m := NewTerminalModel()
	m.width = 20
	m.height = 5
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")
	termWidth, termHeight := m.terminalContentSize()

	msg := m.attachToSession(ws, TerminalTabID("term-tab-reattach"), "session-1", true, "reattach")()
	reattach, ok := msg.(SidebarTerminalReattachResult)
	if !ok {
		t.Fatalf("expected SidebarTerminalReattachResult, got %T", msg)
	}
	if !reattach.CaptureFullPane {
		t.Fatal("expected reattach to carry a full-pane snapshot")
	}
	if got := string(reattach.ScrollbackCapture); got != "pre-attach frame" {
		t.Fatalf("expected pre-attach snapshot data, got %q", got)
	}
	if got := string(reattach.PostAttachScrollbackCapture); got != "fallback" {
		t.Fatalf("expected post-attach history reconciliation capture, got %q", got)
	}
	if reattach.SnapshotCols != 77 || reattach.SnapshotRows != 19 {
		t.Fatalf("expected actual snapshot size 77x19, got %dx%d", reattach.SnapshotCols, reattach.SnapshotRows)
	}
	assertSidebarSnapshotOrder(t, calls, termWidth, termHeight)
}
