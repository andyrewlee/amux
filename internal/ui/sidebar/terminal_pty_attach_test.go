package sidebar

import (
	"errors"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestCreateTerminalTab_FallsBackToHistoryWhenPreAttachResizeFails(t *testing.T) {
	oldEnsureTmuxAvailableFn := ensureTmuxAvailableFn
	oldSessionStateForFn := sessionStateForFn
	oldSessionHasClientsFn := sessionHasClientsFn
	oldSessionActiveWithinFn := sessionActiveWithinFn
	oldSessionCreatedAtFn := sessionCreatedAtFn
	oldSessionPaneIDFn := sessionPaneIDFn
	oldSessionPaneSnapshotInfoFn := sessionPaneSnapshotInfoFn
	oldSessionPaneSizeFn := sessionPaneSizeFn
	oldNewPTYWithSizeFn := newPTYWithSizeFn
	oldResizePaneToSizeFn := resizePaneToSizeFn
	oldCapturePaneSnapshotFn := capturePaneSnapshotFn
	oldCapturePaneFn := capturePaneFn
	oldVerifyTerminalSessionTagsFn := verifyTerminalSessionTagsFn
	defer func() {
		ensureTmuxAvailableFn = oldEnsureTmuxAvailableFn
		sessionStateForFn = oldSessionStateForFn
		sessionHasClientsFn = oldSessionHasClientsFn
		sessionActiveWithinFn = oldSessionActiveWithinFn
		sessionCreatedAtFn = oldSessionCreatedAtFn
		sessionPaneIDFn = oldSessionPaneIDFn
		sessionPaneSnapshotInfoFn = oldSessionPaneSnapshotInfoFn
		sessionPaneSizeFn = oldSessionPaneSizeFn
		newPTYWithSizeFn = oldNewPTYWithSizeFn
		resizePaneToSizeFn = oldResizePaneToSizeFn
		capturePaneSnapshotFn = oldCapturePaneSnapshotFn
		capturePaneFn = oldCapturePaneFn
		verifyTerminalSessionTagsFn = oldVerifyTerminalSessionTagsFn
	}()

	calls := make([]string, 0, 5)
	ensureTmuxAvailableFn = func() error {
		return nil
	}
	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		calls = append(calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	sessionHasClientsFn = func(sessionName string, opts tmux.Options) (bool, error) {
		calls = append(calls, "clients")
		return false, nil
	}
	sessionActiveWithinFn = func(sessionName string, window time.Duration, opts tmux.Options) (bool, error) {
		calls = append(calls, "activity")
		return false, nil
	}
	sessionCreatedAtFn = func(sessionName string, opts tmux.Options) (int64, error) {
		return 123, nil
	}
	sessionPaneIDFn = func(sessionName string, opts tmux.Options) (string, error) {
		return "%1", nil
	}
	sessionPaneSnapshotInfoFn = func(sessionName string, opts tmux.Options) (int, int, bool, error) {
		calls = append(calls, "info")
		return 91, 27, true, nil
	}
	sessionPaneSizeFn = func(sessionName string, opts tmux.Options) (int, int, bool, error) {
		calls = append(calls, "size")
		return 123, 45, true, nil
	}
	resizePaneToSizeFn = func(sessionName string, cols, rows int, opts tmux.Options) error {
		calls = append(calls, "resize")
		return errors.New("resize failed")
	}
	capturePaneSnapshotFn = func(sessionName string, opts tmux.Options) (tmux.PaneSnapshot, error) {
		calls = append(calls, "snapshot")
		return tmux.PaneSnapshot{Data: []byte("should not use")}, nil
	}
	capturePaneFn = func(sessionName string, opts tmux.Options) ([]byte, error) {
		calls = append(calls, "scrollback")
		return []byte("history only"), nil
	}
	newPTYWithSizeFn = func(command, dir string, env []string, rows, cols uint16) (*pty.Terminal, error) {
		calls = append(calls, "attach")
		return &pty.Terminal{}, nil
	}
	verifyTerminalSessionTagsFn = func(sessionName string, tags tmux.SessionTags, opts tmux.Options) error {
		calls = append(calls, "verify")
		return nil
	}

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
	if got := string(created.Scrollback); got != "history only" {
		t.Fatalf("expected history-only fallback, got %q", got)
	}
	if created.CaptureCols != 123 || created.CaptureRows != 45 {
		t.Fatalf("expected history-only capture size 123x45, got %dx%d", created.CaptureCols, created.CaptureRows)
	}
	attachIdx := -1
	scrollbackIdx := -1
	for i, call := range calls {
		if call == "attach" {
			attachIdx = i
		}
		if call == "scrollback" {
			scrollbackIdx = i
		}
	}
	if attachIdx == -1 || scrollbackIdx == -1 {
		t.Fatalf("expected attach and post-attach scrollback capture, got %v", calls)
	}
	if scrollbackIdx < attachIdx {
		t.Fatalf("expected fallback scrollback capture after attach, got call order %v", calls)
	}
	snapshotCount := 0
	for _, call := range calls {
		if call == "snapshot" {
			snapshotCount++
		}
	}
	if snapshotCount != 0 {
		t.Fatalf("expected resize failure to prevent any full snapshot capture, got %v", calls)
	}
}

func TestAttachToSession_FallsBackToHistoryWhenPreAttachResizeFails(t *testing.T) {
	oldEnsureTmuxAvailableFn := ensureTmuxAvailableFn
	oldSessionStateForFn := sessionStateForFn
	oldSessionHasClientsFn := sessionHasClientsFn
	oldSessionActiveWithinFn := sessionActiveWithinFn
	oldSessionCreatedAtFn := sessionCreatedAtFn
	oldSessionPaneIDFn := sessionPaneIDFn
	oldSessionPaneSnapshotInfoFn := sessionPaneSnapshotInfoFn
	oldSessionPaneSizeFn := sessionPaneSizeFn
	oldNewPTYWithSizeFn := newPTYWithSizeFn
	oldResizePaneToSizeFn := resizePaneToSizeFn
	oldCapturePaneSnapshotFn := capturePaneSnapshotFn
	oldCapturePaneFn := capturePaneFn
	oldVerifyTerminalSessionTagsFn := verifyTerminalSessionTagsFn
	defer func() {
		ensureTmuxAvailableFn = oldEnsureTmuxAvailableFn
		sessionStateForFn = oldSessionStateForFn
		sessionHasClientsFn = oldSessionHasClientsFn
		sessionActiveWithinFn = oldSessionActiveWithinFn
		sessionCreatedAtFn = oldSessionCreatedAtFn
		sessionPaneIDFn = oldSessionPaneIDFn
		sessionPaneSnapshotInfoFn = oldSessionPaneSnapshotInfoFn
		sessionPaneSizeFn = oldSessionPaneSizeFn
		newPTYWithSizeFn = oldNewPTYWithSizeFn
		resizePaneToSizeFn = oldResizePaneToSizeFn
		capturePaneSnapshotFn = oldCapturePaneSnapshotFn
		capturePaneFn = oldCapturePaneFn
		verifyTerminalSessionTagsFn = oldVerifyTerminalSessionTagsFn
	}()

	calls := make([]string, 0, 5)
	ensureTmuxAvailableFn = func() error {
		return nil
	}
	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		calls = append(calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	sessionHasClientsFn = func(sessionName string, opts tmux.Options) (bool, error) {
		calls = append(calls, "clients")
		return false, nil
	}
	sessionActiveWithinFn = func(sessionName string, window time.Duration, opts tmux.Options) (bool, error) {
		calls = append(calls, "activity")
		return false, nil
	}
	sessionCreatedAtFn = func(sessionName string, opts tmux.Options) (int64, error) {
		return 123, nil
	}
	sessionPaneIDFn = func(sessionName string, opts tmux.Options) (string, error) {
		return "%1", nil
	}
	sessionPaneSnapshotInfoFn = func(sessionName string, opts tmux.Options) (int, int, bool, error) {
		calls = append(calls, "info")
		return 91, 27, true, nil
	}
	sessionPaneSizeFn = func(sessionName string, opts tmux.Options) (int, int, bool, error) {
		calls = append(calls, "size")
		return 123, 45, true, nil
	}
	resizePaneToSizeFn = func(sessionName string, cols, rows int, opts tmux.Options) error {
		calls = append(calls, "resize")
		return errors.New("resize failed")
	}
	capturePaneSnapshotFn = func(sessionName string, opts tmux.Options) (tmux.PaneSnapshot, error) {
		calls = append(calls, "snapshot")
		return tmux.PaneSnapshot{Data: []byte("should not use")}, nil
	}
	capturePaneFn = func(sessionName string, opts tmux.Options) ([]byte, error) {
		calls = append(calls, "scrollback")
		return []byte("history only"), nil
	}
	newPTYWithSizeFn = func(command, dir string, env []string, rows, cols uint16) (*pty.Terminal, error) {
		calls = append(calls, "attach")
		return &pty.Terminal{}, nil
	}
	verifyTerminalSessionTagsFn = func(sessionName string, tags tmux.SessionTags, opts tmux.Options) error {
		calls = append(calls, "verify")
		return nil
	}

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
	if got := string(reattach.Scrollback); got != "history only" {
		t.Fatalf("expected history-only fallback, got %q", got)
	}
	if reattach.CaptureCols != 123 || reattach.CaptureRows != 45 {
		t.Fatalf("expected history-only capture size 123x45, got %dx%d", reattach.CaptureCols, reattach.CaptureRows)
	}
	attachIdx := -1
	scrollbackIdx := -1
	for i, call := range calls {
		if call == "attach" {
			attachIdx = i
		}
		if call == "scrollback" {
			scrollbackIdx = i
		}
	}
	if attachIdx == -1 || scrollbackIdx == -1 {
		t.Fatalf("expected attach and post-attach scrollback capture, got %v", calls)
	}
	if scrollbackIdx < attachIdx {
		t.Fatalf("expected fallback scrollback capture after attach, got call order %v", calls)
	}
	snapshotCount := 0
	for _, call := range calls {
		if call == "snapshot" {
			snapshotCount++
		}
	}
	if snapshotCount != 0 {
		t.Fatalf("expected resize failure to prevent any full snapshot capture, got %v", calls)
	}
}
