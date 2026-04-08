package sidebar

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestCreateTerminalTab_DiscardsPreAttachSnapshotWhenSessionRecreated(t *testing.T) {
	oldEnsureTmuxAvailableFn := ensureTmuxAvailableFn
	oldSessionStateForFn := sessionStateForFn
	oldSessionHasClientsFn := sessionHasClientsFn
	oldSessionClientCountFn := sessionClientCountFn
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
		sessionClientCountFn = oldSessionClientCountFn
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

	calls := make([]string, 0, 8)
	ensureTmuxAvailableFn = func() error { return nil }
	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		calls = append(calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	sessionHasClientsFn = func(sessionName string, opts tmux.Options) (bool, error) {
		calls = append(calls, "clients")
		return false, nil
	}
	sessionClientCountFn = func(sessionName string, opts tmux.Options) (int, error) {
		return 1, nil
	}
	sessionActiveWithinFn = func(sessionName string, window time.Duration, opts tmux.Options) (bool, error) {
		calls = append(calls, "activity")
		return false, nil
	}
	createdAtCalls := 0
	sessionCreatedAtFn = func(sessionName string, opts tmux.Options) (int64, error) {
		createdAtCalls++
		return 111, nil
	}
	paneIDCalls := 0
	sessionPaneIDFn = func(sessionName string, opts tmux.Options) (string, error) {
		paneIDCalls++
		if paneIDCalls <= 4 {
			return "%old", nil
		}
		return "%new", nil
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
		return nil
	}
	capturePaneSnapshotFn = func(sessionName string, opts tmux.Options) (tmux.PaneSnapshot, error) {
		calls = append(calls, "snapshot")
		return tmux.PaneSnapshot{Data: []byte("stale frame"), Cols: 77, Rows: 19}, nil
	}
	newPTYWithSizeFn = func(command, dir string, env []string, rows, cols uint16) (*pty.Terminal, error) {
		calls = append(calls, "attach")
		return &pty.Terminal{}, nil
	}
	capturePaneFn = func(sessionName string, opts tmux.Options) ([]byte, error) {
		calls = append(calls, "scrollback")
		return []byte("post history"), nil
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
		t.Fatal("expected recreated session to discard the pre-attach snapshot")
	}
	if got := string(created.Scrollback); got != "post history" {
		t.Fatalf("expected post-attach history recapture after stale snapshot demotion, got %q", got)
	}
	if len(created.PostAttachScrollback) != 0 {
		t.Fatalf("expected no reconciliation capture after demoting to history-only restore, got %q", string(created.PostAttachScrollback))
	}
	if created.CaptureCols != 123 || created.CaptureRows != 45 {
		t.Fatalf("expected history-only capture size 123x45, got %dx%d", created.CaptureCols, created.CaptureRows)
	}
	if created.SnapshotCols != 0 || created.SnapshotRows != 0 {
		t.Fatalf("expected stale snapshot metadata to be cleared, got %dx%d", created.SnapshotCols, created.SnapshotRows)
	}
	assertSidebarCallOrder(t, calls, "state", "clients", "activity", "info", "resize", "snapshot", "attach", "size", "scrollback")
}

func TestAttachToSession_DiscardsPreAttachSnapshotWhenSessionRecreated(t *testing.T) {
	oldEnsureTmuxAvailableFn := ensureTmuxAvailableFn
	oldSessionStateForFn := sessionStateForFn
	oldSessionHasClientsFn := sessionHasClientsFn
	oldSessionClientCountFn := sessionClientCountFn
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
		sessionClientCountFn = oldSessionClientCountFn
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

	calls := make([]string, 0, 8)
	ensureTmuxAvailableFn = func() error { return nil }
	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		calls = append(calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	sessionHasClientsFn = func(sessionName string, opts tmux.Options) (bool, error) {
		calls = append(calls, "clients")
		return false, nil
	}
	sessionClientCountFn = func(sessionName string, opts tmux.Options) (int, error) {
		return 1, nil
	}
	sessionActiveWithinFn = func(sessionName string, window time.Duration, opts tmux.Options) (bool, error) {
		calls = append(calls, "activity")
		return false, nil
	}
	createdAtCalls := 0
	sessionCreatedAtFn = func(sessionName string, opts tmux.Options) (int64, error) {
		createdAtCalls++
		return 111, nil
	}
	paneIDCalls := 0
	sessionPaneIDFn = func(sessionName string, opts tmux.Options) (string, error) {
		paneIDCalls++
		if paneIDCalls <= 4 {
			return "%old", nil
		}
		return "%new", nil
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
		return nil
	}
	capturePaneSnapshotFn = func(sessionName string, opts tmux.Options) (tmux.PaneSnapshot, error) {
		calls = append(calls, "snapshot")
		return tmux.PaneSnapshot{Data: []byte("stale frame"), Cols: 77, Rows: 19}, nil
	}
	newPTYWithSizeFn = func(command, dir string, env []string, rows, cols uint16) (*pty.Terminal, error) {
		calls = append(calls, "attach")
		return &pty.Terminal{}, nil
	}
	capturePaneFn = func(sessionName string, opts tmux.Options) ([]byte, error) {
		calls = append(calls, "scrollback")
		return []byte("post history"), nil
	}
	verifyTerminalSessionTagsFn = func(sessionName string, tags tmux.SessionTags, opts tmux.Options) error {
		calls = append(calls, "verify")
		return nil
	}

	m := NewTerminalModel()
	m.width = 20
	m.height = 5
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")

	msg := m.attachToSession(ws, TerminalTabID("term-tab-race"), "session-race", true, "reattach")()
	reattach, ok := msg.(SidebarTerminalReattachResult)
	if !ok {
		t.Fatalf("expected SidebarTerminalReattachResult, got %T", msg)
	}
	if reattach.CaptureFullPane {
		t.Fatal("expected recreated session to discard the pre-attach snapshot")
	}
	if got := string(reattach.Scrollback); got != "post history" {
		t.Fatalf("expected post-attach history recapture after stale snapshot demotion, got %q", got)
	}
	if len(reattach.PostAttachScrollback) != 0 {
		t.Fatalf("expected no reconciliation capture after demoting to history-only restore, got %q", string(reattach.PostAttachScrollback))
	}
	if reattach.CaptureCols != 123 || reattach.CaptureRows != 45 {
		t.Fatalf("expected history-only capture size 123x45, got %dx%d", reattach.CaptureCols, reattach.CaptureRows)
	}
	if reattach.SnapshotCols != 0 || reattach.SnapshotRows != 0 {
		t.Fatalf("expected stale snapshot metadata to be cleared, got %dx%d", reattach.SnapshotCols, reattach.SnapshotRows)
	}
	assertSidebarCallOrder(t, calls, "state", "clients", "activity", "info", "resize", "snapshot", "attach", "size", "scrollback")
}

func TestAttachToSession_DiscardsPreAttachSnapshotWhenSessionBecomesActive(t *testing.T) {
	oldEnsureTmuxAvailableFn := ensureTmuxAvailableFn
	oldSessionStateForFn := sessionStateForFn
	oldSessionHasClientsFn := sessionHasClientsFn
	oldSessionClientCountFn := sessionClientCountFn
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
		sessionClientCountFn = oldSessionClientCountFn
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

	calls := make([]string, 0, 12)
	ensureTmuxAvailableFn = func() error { return nil }
	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		calls = append(calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	sessionHasClientsFn = func(sessionName string, opts tmux.Options) (bool, error) {
		calls = append(calls, "clients")
		return false, nil
	}
	sessionClientCountFn = func(sessionName string, opts tmux.Options) (int, error) {
		return 1, nil
	}
	activityCalls := 0
	sessionActiveWithinFn = func(sessionName string, window time.Duration, opts tmux.Options) (bool, error) {
		calls = append(calls, "activity")
		activityCalls++
		return activityCalls >= 3, nil
	}
	sessionCreatedAtFn = func(sessionName string, opts tmux.Options) (int64, error) {
		return 111, nil
	}
	sessionPaneIDFn = func(sessionName string, opts tmux.Options) (string, error) {
		return "%same", nil
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
		return nil
	}
	capturePaneSnapshotFn = func(sessionName string, opts tmux.Options) (tmux.PaneSnapshot, error) {
		calls = append(calls, "snapshot")
		return tmux.PaneSnapshot{Data: []byte("stale frame"), Cols: 77, Rows: 19}, nil
	}
	newPTYWithSizeFn = func(command, dir string, env []string, rows, cols uint16) (*pty.Terminal, error) {
		calls = append(calls, "attach")
		return &pty.Terminal{}, nil
	}
	capturePaneFn = func(sessionName string, opts tmux.Options) ([]byte, error) {
		calls = append(calls, "scrollback")
		return []byte("post history"), nil
	}
	verifyTerminalSessionTagsFn = func(sessionName string, tags tmux.SessionTags, opts tmux.Options) error {
		calls = append(calls, "verify")
		return nil
	}

	m := NewTerminalModel()
	m.width = 20
	m.height = 5
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")

	msg := m.attachToSession(ws, TerminalTabID("term-tab-active-race"), "session-race", true, "reattach")()
	reattach, ok := msg.(SidebarTerminalReattachResult)
	if !ok {
		t.Fatalf("expected SidebarTerminalReattachResult, got %T", msg)
	}
	if reattach.CaptureFullPane {
		t.Fatal("expected activity after snapshot capture to demote sidebar restore back to history-only")
	}
	if got := string(reattach.Scrollback); got != "post history" {
		t.Fatalf("expected post-attach history recapture after stale snapshot demotion, got %q", got)
	}
	if len(reattach.PostAttachScrollback) != 0 {
		t.Fatalf("expected no reconciliation delta after stale snapshot demotion, got %q", string(reattach.PostAttachScrollback))
	}
	assertSidebarCallOrder(t, calls, "state", "clients", "activity", "info", "resize", "snapshot", "attach", "size", "scrollback")
}

func TestAttachToSession_DiscardsPreAttachSnapshotWhenSessionBecomesShared(t *testing.T) {
	oldEnsureTmuxAvailableFn := ensureTmuxAvailableFn
	oldSessionStateForFn := sessionStateForFn
	oldSessionHasClientsFn := sessionHasClientsFn
	oldSessionClientCountFn := sessionClientCountFn
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
		sessionClientCountFn = oldSessionClientCountFn
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

	calls := make([]string, 0, 12)
	ensureTmuxAvailableFn = func() error { return nil }
	sessionStateForFn = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		calls = append(calls, "state")
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	sessionHasClientsFn = func(sessionName string, opts tmux.Options) (bool, error) {
		calls = append(calls, "clients")
		return false, nil
	}
	sessionClientCountFn = func(sessionName string, opts tmux.Options) (int, error) {
		return 2, nil
	}
	sessionActiveWithinFn = func(sessionName string, window time.Duration, opts tmux.Options) (bool, error) {
		calls = append(calls, "activity")
		return false, nil
	}
	sessionCreatedAtFn = func(sessionName string, opts tmux.Options) (int64, error) {
		return 111, nil
	}
	sessionPaneIDFn = func(sessionName string, opts tmux.Options) (string, error) {
		return "%same", nil
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
		return nil
	}
	capturePaneSnapshotFn = func(sessionName string, opts tmux.Options) (tmux.PaneSnapshot, error) {
		calls = append(calls, "snapshot")
		return tmux.PaneSnapshot{Data: []byte("stale frame"), Cols: 77, Rows: 19}, nil
	}
	newPTYWithSizeFn = func(command, dir string, env []string, rows, cols uint16) (*pty.Terminal, error) {
		calls = append(calls, "attach")
		return &pty.Terminal{}, nil
	}
	capturePaneFn = func(sessionName string, opts tmux.Options) ([]byte, error) {
		calls = append(calls, "scrollback")
		return []byte("post history"), nil
	}
	verifyTerminalSessionTagsFn = func(sessionName string, tags tmux.SessionTags, opts tmux.Options) error {
		calls = append(calls, "verify")
		return nil
	}

	m := NewTerminalModel()
	m.width = 20
	m.height = 5
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")

	msg := m.attachToSession(ws, TerminalTabID("term-tab-shared-race"), "session-race", true, "reattach")()
	reattach, ok := msg.(SidebarTerminalReattachResult)
	if !ok {
		t.Fatalf("expected SidebarTerminalReattachResult, got %T", msg)
	}
	if reattach.CaptureFullPane {
		t.Fatal("expected shared sidebar session after snapshot capture to demote back to history-only")
	}
	if got := string(reattach.Scrollback); got != "post history" {
		t.Fatalf("expected post-attach history recapture after shared-session demotion, got %q", got)
	}
	if len(reattach.PostAttachScrollback) != 0 {
		t.Fatalf("expected no reconciliation delta after shared-session demotion, got %q", string(reattach.PostAttachScrollback))
	}
	assertSidebarCallOrder(t, calls, "state", "clients", "activity", "info", "resize", "snapshot", "attach", "size", "scrollback")
}
