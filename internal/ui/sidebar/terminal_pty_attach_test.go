package sidebar

import (
	"errors"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestCreateTerminalTabRejectsInvalidShell(t *testing.T) {
	oldNewPTYWithSizeFn := newPTYWithSizeFn
	defer func() {
		newPTYWithSizeFn = oldNewPTYWithSizeFn
	}()
	t.Setenv("SHELL", "zsh")

	newPTYWithSizeFn = func(command, dir string, env []string, rows, cols uint16) (*pty.Terminal, error) {
		t.Fatal("newPTYWithSizeFn should not be called with invalid SHELL")
		return nil, nil
	}

	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")

	msg := m.createTerminalTab(ws)()
	failed, ok := msg.(SidebarTerminalCreateFailed)
	if !ok {
		t.Fatalf("expected SidebarTerminalCreateFailed, got %T", msg)
	}
	if failed.Err == nil || !strings.Contains(failed.Err.Error(), "absolute path") {
		t.Fatalf("expected absolute-path error, got %v", failed.Err)
	}
}

func TestAttachToSessionRejectsInvalidShell(t *testing.T) {
	oldNewPTYWithSizeFn := newPTYWithSizeFn
	defer func() {
		newPTYWithSizeFn = oldNewPTYWithSizeFn
	}()
	t.Setenv("SHELL", "zsh")

	newPTYWithSizeFn = func(command, dir string, env []string, rows, cols uint16) (*pty.Terminal, error) {
		t.Fatal("newPTYWithSizeFn should not be called with invalid SHELL")
		return nil, nil
	}

	m := NewTerminalModel()
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")

	msg := m.attachToSession(ws, TerminalTabID("term-tab-invalid-shell"), "session-1", true, "reattach")()
	failed, ok := msg.(SidebarTerminalReattachFailed)
	if !ok {
		t.Fatalf("expected SidebarTerminalReattachFailed, got %T", msg)
	}
	if failed.Err == nil || !strings.Contains(failed.Err.Error(), "absolute path") {
		t.Fatalf("expected absolute-path error, got %v", failed.Err)
	}
}

// installResizeFailureSeams wires an otherwise-eligible session whose pre-attach
// resize fails. The bootstrap must abandon the snapshot at that point: it never
// got the pane to the client's size, so a capture would describe the wrong
// geometry.
func installResizeFailureSeams(t *testing.T, calls *[]string) {
	t.Helper()
	restoreAttachSeams(t)

	ensureTmuxAvailableFn = func() error { return nil }
	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		*calls = append(*calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	probeSeq(calls, eligibleAttachProbe())
	resizePaneToSizeFn = func(string, int, int, tmux.Options) error {
		*calls = append(*calls, "resize")
		return errors.New("resize failed")
	}
	capturePaneFullDataFn = func(string, tmux.Options) ([]byte, error) {
		*calls = append(*calls, "snapshot")
		return []byte("should not use"), nil
	}
	capturePaneHistoryDataFn = func(string, tmux.Options) ([]byte, error) {
		*calls = append(*calls, "scrollback")
		return []byte("history only"), nil
	}
	capturePaneFn = func(string, tmux.Options) ([]byte, error) {
		*calls = append(*calls, "scrollback")
		return []byte("history only"), nil
	}
	newPTYWithSizeFn = func(command, dir string, env []string, rows, cols uint16) (*pty.Terminal, error) {
		*calls = append(*calls, "attach")
		return &pty.Terminal{}, nil
	}
	verifyTerminalSessionTagsFn = func(sessionName string, tags tmux.SessionTags, opts tmux.Options) error {
		*calls = append(*calls, "verify")
		return nil
	}
}

// assertResizeFailureFallback asserts the history fallback ran after the attach
// and that no full-pane capture was attempted.
func assertResizeFailureFallback(t *testing.T, calls []string) {
	t.Helper()
	attachIdx, scrollbackIdx, snapshotCount := -1, -1, 0
	for i, call := range calls {
		switch call {
		case "attach":
			attachIdx = i
		case "scrollback":
			scrollbackIdx = i
		case "snapshot":
			snapshotCount++
		}
	}
	if attachIdx == -1 || scrollbackIdx == -1 {
		t.Fatalf("expected attach and post-attach scrollback capture, got %v", calls)
	}
	if scrollbackIdx < attachIdx {
		t.Fatalf("expected fallback scrollback capture after attach, got call order %v", calls)
	}
	if snapshotCount != 0 {
		t.Fatalf("expected resize failure to prevent any full snapshot capture, got %v", calls)
	}
}

func TestCreateTerminalTab_FallsBackToHistoryWhenPreAttachResizeFails(t *testing.T) {
	var calls []string
	installResizeFailureSeams(t, &calls)

	m := NewTerminalModel()
	m.width = 20
	m.height = 5
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")

	msg := m.createTerminalTab(ws)()
	created, ok := msg.(SidebarTerminalCreated)
	if !ok {
		t.Fatalf("expected SidebarTerminalCreated, got %T", msg)
	}
	if created.CaptureFullPane {
		t.Fatal("expected resize failure to disable authoritative full-pane restore")
	}
	if got := string(created.ScrollbackCapture); got != "history only" {
		t.Fatalf("expected history-only fallback, got %q", got)
	}
	if created.CaptureCols != 123 || created.CaptureRows != 45 {
		t.Fatalf("expected history-only capture size 123x45, got %dx%d", created.CaptureCols, created.CaptureRows)
	}
	assertResizeFailureFallback(t, calls)
}

func TestAttachToSession_FallsBackToHistoryWhenPreAttachResizeFails(t *testing.T) {
	var calls []string
	installResizeFailureSeams(t, &calls)

	m := NewTerminalModel()
	m.width = 20
	m.height = 5
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")

	msg := m.attachToSession(ws, TerminalTabID("term-tab-reattach"), "session-1", true, "reattach")()
	reattach, ok := msg.(SidebarTerminalReattachResult)
	if !ok {
		t.Fatalf("expected SidebarTerminalReattachResult, got %T", msg)
	}
	if reattach.CaptureFullPane {
		t.Fatal("expected resize failure to disable authoritative full-pane restore")
	}
	if got := string(reattach.ScrollbackCapture); got != "history only" {
		t.Fatalf("expected history-only fallback, got %q", got)
	}
	if reattach.CaptureCols != 123 || reattach.CaptureRows != 45 {
		t.Fatalf("expected history-only capture size 123x45, got %dx%d", reattach.CaptureCols, reattach.CaptureRows)
	}
	assertResizeFailureFallback(t, calls)
}
