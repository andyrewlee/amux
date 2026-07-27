package sidebar

import (
	"errors"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

// installSidebarHistoryFallbackSeams wires a live session whose probe reports
// the given ineligibility, so the bootstrap must fall back to replaying history
// without ever resizing or snapshotting the pane.
func installSidebarHistoryFallbackSeams(t *testing.T, calls *[]string, probe tmux.SessionProbe) {
	t.Helper()
	restoreAttachSeams(t)

	ensureTmuxAvailableFn = func() error { return nil }
	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		*calls = append(*calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	probeSeq(calls, probe)
	resizePaneToSizeFn = func(string, int, int, tmux.Options) error {
		*calls = append(*calls, "resize")
		return nil
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

// assertNoPreAttachMutation asserts the fallback path never resized or
// snapshotted the pane.
func assertNoPreAttachMutation(t *testing.T, calls []string) {
	t.Helper()
	for _, call := range calls {
		if call == "resize" || call == "snapshot" {
			t.Fatalf("expected an ineligible session to avoid pre-attach resize/snapshot, got %v", calls)
		}
	}
}

func TestAttachToSession_SharedClientFallsBackToHistoryOnly(t *testing.T) {
	var calls []string
	shared := eligibleAttachProbe()
	shared.ClientCount = 1
	installSidebarHistoryFallbackSeams(t, &calls, shared)

	m := NewTerminalModel()
	m.width = 20
	m.height = 5
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")

	msg := m.attachToSession(ws, TerminalTabID("term-tab-shared"), "session-shared", false, "reattach")()
	reattach, ok := msg.(SidebarTerminalReattachResult)
	if !ok {
		t.Fatalf("expected SidebarTerminalReattachResult, got %T", msg)
	}
	if reattach.CaptureFullPane {
		t.Fatal("expected shared-client reattach to skip pre-attach full-pane restore")
	}
	if got := string(reattach.ScrollbackCapture); got != "history only" {
		t.Fatalf("expected history-only fallback, got %q", got)
	}
	if reattach.CaptureCols != 123 || reattach.CaptureRows != 45 {
		t.Fatalf("expected history-only capture size 123x45, got %dx%d", reattach.CaptureCols, reattach.CaptureRows)
	}

	// The history capture has to happen after the attach: it is what reconciles
	// anything the session emitted while the client was connecting.
	attachIdx, scrollbackIdx := -1, -1
	for i, call := range calls {
		switch call {
		case "attach":
			attachIdx = i
		case "scrollback":
			scrollbackIdx = i
		}
	}
	if attachIdx == -1 || scrollbackIdx == -1 {
		t.Fatalf("expected attach and post-attach scrollback capture, got %v", calls)
	}
	if scrollbackIdx < attachIdx {
		t.Fatalf("expected shared-client fallback scrollback capture after attach, got call order %v", calls)
	}
	assertNoPreAttachMutation(t, calls)
}

func TestCreateTerminalTab_SnapshotIneligibleFallsBackWithoutResize(t *testing.T) {
	var calls []string
	// A pane with no VT mode metadata cannot anchor an authoritative snapshot.
	ineligible := eligibleAttachProbe()
	ineligible.PaneMeta.ModeState = tmux.PaneModeState{}
	installSidebarHistoryFallbackSeams(t, &calls, ineligible)

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
		t.Fatal("expected ineligible snapshot to disable authoritative full-pane restore")
	}
	if got := string(created.ScrollbackCapture); got != "history only" {
		t.Fatalf("expected history-only fallback, got %q", got)
	}
	if created.CaptureCols != 123 || created.CaptureRows != 45 {
		t.Fatalf("expected history-only capture size 123x45, got %dx%d", created.CaptureCols, created.CaptureRows)
	}
	assertNoPreAttachMutation(t, calls)
}

func TestAttachToSession_SnapshotIneligibleFallsBackWithoutResize(t *testing.T) {
	var calls []string
	ineligible := eligibleAttachProbe()
	ineligible.PaneMeta.ModeState = tmux.PaneModeState{}
	installSidebarHistoryFallbackSeams(t, &calls, ineligible)

	m := NewTerminalModel()
	m.width = 20
	m.height = 5
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")

	msg := m.attachToSession(ws, TerminalTabID("term-tab-ineligible"), "session-ineligible", true, "reattach")()
	reattach, ok := msg.(SidebarTerminalReattachResult)
	if !ok {
		t.Fatalf("expected SidebarTerminalReattachResult, got %T", msg)
	}
	if reattach.CaptureFullPane {
		t.Fatal("expected ineligible snapshot to disable authoritative full-pane restore")
	}
	if got := string(reattach.ScrollbackCapture); got != "history only" {
		t.Fatalf("expected history-only fallback, got %q", got)
	}
	if reattach.CaptureCols != 123 || reattach.CaptureRows != 45 {
		t.Fatalf("expected history-only capture size 123x45, got %dx%d", reattach.CaptureCols, reattach.CaptureRows)
	}
	assertNoPreAttachMutation(t, calls)
}

func TestAttachToSession_AttachFailureRollsBackBootstrapResize(t *testing.T) {
	var calls []string
	restoreAttachSeams(t)

	ensureTmuxAvailableFn = func() error { return nil }
	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		calls = append(calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	// Still unattached and unchanged at the rollback probe, so the rollback is
	// safe to perform.
	probeSeq(&calls, eligibleAttachProbe())
	resizePaneToSizeFn = func(string, int, int, tmux.Options) error {
		calls = append(calls, "resize")
		return nil
	}
	capturePaneFullDataFn = func(string, tmux.Options) ([]byte, error) {
		calls = append(calls, "snapshot")
		return []byte("resized"), nil
	}
	newPTYWithSizeFn = func(command, dir string, env []string, rows, cols uint16) (*pty.Terminal, error) {
		calls = append(calls, "attach")
		return nil, errors.New("attach failed")
	}
	verifyTerminalSessionTagsFn = func(sessionName string, tags tmux.SessionTags, opts tmux.Options) error {
		calls = append(calls, "verify")
		return nil
	}

	m := NewTerminalModel()
	m.width = 20
	m.height = 5
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")

	msg := m.attachToSession(ws, TerminalTabID("term-tab-rollback"), "session-rollback", true, "reattach")()
	failed, ok := msg.(SidebarTerminalReattachFailed)
	if !ok {
		t.Fatalf("expected SidebarTerminalReattachFailed, got %T", msg)
	}
	if failed.Err == nil || failed.Err.Error() != "attach failed" {
		t.Fatalf("expected attach failure, got %+v", failed)
	}
	// The trailing resize is the rollback: a failed attach must not leave the
	// pane at the size the aborted bootstrap set.
	assertSidebarCallOrder(t, calls, "probe", "resize", "snapshot", "attach", "resize")
}

func assertSidebarCallOrder(t *testing.T, calls []string, expected ...string) {
	t.Helper()
	last := -1
	for _, want := range expected {
		idx := -1
		for i := last + 1; i < len(calls); i++ {
			if calls[i] == want {
				idx = i
				break
			}
		}
		if idx == -1 {
			t.Fatalf("expected call %q, got %v", want, calls)
		}
		last = idx
	}
}
