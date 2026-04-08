package sidebar

import (
	"fmt"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestCreateTerminalTab_CapturesReusedSessionBeforeAttachAfterResize(t *testing.T) {
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

	calls := make([]string, 0, 4)
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
		return 77, 19, true, nil
	}
	resizePaneToSizeFn = func(sessionName string, cols, rows int, opts tmux.Options) error {
		calls = append(calls, fmt.Sprintf("resize:%dx%d", cols, rows))
		return nil
	}
	newPTYWithSizeFn = func(command, dir string, env []string, rows, cols uint16) (*pty.Terminal, error) {
		calls = append(calls, fmt.Sprintf("attach:%dx%d", cols, rows))
		return &pty.Terminal{}, nil
	}
	capturePaneSnapshotFn = func(sessionName string, opts tmux.Options) (tmux.PaneSnapshot, error) {
		calls = append(calls, "snapshot")
		return tmux.PaneSnapshot{Data: []byte("resized frame"), Cols: 77, Rows: 19}, nil
	}
	capturePaneFn = func(sessionName string, opts tmux.Options) ([]byte, error) {
		calls = append(calls, "scrollback")
		return []byte("fallback"), nil
	}
	verifyTerminalSessionTagsFn = func(sessionName string, tags tmux.SessionTags, opts tmux.Options) error {
		calls = append(calls, "verify")
		return nil
	}

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
	if got := string(created.Scrollback); got != "resized frame" {
		t.Fatalf("expected snapshot data from pre-attach capture, got %q", got)
	}
	if got := string(created.PostAttachScrollback); got != "fallback" {
		t.Fatalf("expected post-attach history reconciliation capture, got %q", got)
	}
	if created.SnapshotCols != 77 || created.SnapshotRows != 19 {
		t.Fatalf("expected actual snapshot size 77x19, got %dx%d", created.SnapshotCols, created.SnapshotRows)
	}

	expectedAttach := fmt.Sprintf("attach:%dx%d", termWidth, termHeight)
	expectedResize := fmt.Sprintf("resize:%dx%d", termWidth, termHeight)
	clientsIdx := -1
	activityIdx := -1
	attachIdx := -1
	resizeIdx := -1
	for i, call := range calls {
		if call == "clients" {
			clientsIdx = i
		}
		if call == "activity" {
			activityIdx = i
		}
		if call == expectedResize {
			resizeIdx = i
		}
		if call == expectedAttach {
			attachIdx = i
		}
	}
	if attachIdx == -1 {
		t.Fatalf("expected attach call %q, got %v", expectedAttach, calls)
	}
	if clientsIdx == -1 || activityIdx == -1 {
		t.Fatalf("expected safety checks before snapshot, got %v", calls)
	}
	if resizeIdx == -1 {
		t.Fatalf("expected resize call %q, got %v", expectedResize, calls)
	}
	snapshotCount := 0
	infoIdx := -1
	snapshotIdx := -1
	for i, call := range calls {
		if call == "info" {
			infoIdx = i
		}
		if call != "snapshot" {
			continue
		}
		snapshotCount++
		if snapshotIdx == -1 {
			snapshotIdx = i
		}
	}
	if infoIdx == -1 {
		t.Fatalf("expected bootstrap pane metadata lookup, got %v", calls)
	}
	if snapshotCount != 1 {
		t.Fatalf("expected a single resized snapshot capture, got %v", calls)
	}
	if infoIdx > resizeIdx || resizeIdx > snapshotIdx {
		t.Fatalf("expected metadata lookup before resize and final snapshot after resize, got call order %v", calls)
	}
	if snapshotIdx > attachIdx {
		t.Fatalf("expected reused-session snapshot before attach, got call order %v", calls)
	}
	scrollbackIdx := -1
	scrollbackCount := 0
	verifyIdx := -1
	for i, call := range calls {
		if call == "scrollback" {
			scrollbackCount++
			if scrollbackIdx == -1 {
				scrollbackIdx = i
			}
		}
		if call == "verify" {
			verifyIdx = i
		}
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

func TestAttachToSession_CapturesReattachSnapshotBeforeAttach(t *testing.T) {
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

	calls := make([]string, 0, 4)
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
		return 77, 19, true, nil
	}
	resizePaneToSizeFn = func(sessionName string, cols, rows int, opts tmux.Options) error {
		calls = append(calls, fmt.Sprintf("resize:%dx%d", cols, rows))
		return nil
	}
	capturePaneSnapshotFn = func(sessionName string, opts tmux.Options) (tmux.PaneSnapshot, error) {
		calls = append(calls, "snapshot")
		return tmux.PaneSnapshot{Data: []byte("pre-attach frame"), Cols: 77, Rows: 19}, nil
	}
	newPTYWithSizeFn = func(command, dir string, env []string, rows, cols uint16) (*pty.Terminal, error) {
		calls = append(calls, fmt.Sprintf("attach:%dx%d", cols, rows))
		return &pty.Terminal{}, nil
	}
	capturePaneFn = func(sessionName string, opts tmux.Options) ([]byte, error) {
		calls = append(calls, "scrollback")
		return []byte("fallback"), nil
	}
	verifyTerminalSessionTagsFn = func(sessionName string, tags tmux.SessionTags, opts tmux.Options) error {
		calls = append(calls, "verify")
		return nil
	}

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
	if got := string(reattach.Scrollback); got != "pre-attach frame" {
		t.Fatalf("expected pre-attach snapshot data, got %q", got)
	}
	if got := string(reattach.PostAttachScrollback); got != "fallback" {
		t.Fatalf("expected post-attach history reconciliation capture, got %q", got)
	}
	if reattach.SnapshotCols != 77 || reattach.SnapshotRows != 19 {
		t.Fatalf("expected actual snapshot size 77x19, got %dx%d", reattach.SnapshotCols, reattach.SnapshotRows)
	}

	expectedAttach := fmt.Sprintf("attach:%dx%d", termWidth, termHeight)
	expectedResize := fmt.Sprintf("resize:%dx%d", termWidth, termHeight)
	clientsIdx := -1
	activityIdx := -1
	attachIdx := -1
	resizeIdx := -1
	for i, call := range calls {
		if call == "clients" {
			clientsIdx = i
		}
		if call == "activity" {
			activityIdx = i
		}
		if call == expectedAttach {
			attachIdx = i
		}
		if call == expectedResize {
			resizeIdx = i
		}
	}
	if resizeIdx == -1 {
		t.Fatalf("expected reattach resize call %q, got %v", expectedResize, calls)
	}
	if clientsIdx == -1 || activityIdx == -1 {
		t.Fatalf("expected safety checks before pre-attach snapshot, got %v", calls)
	}
	if attachIdx == -1 {
		t.Fatalf("expected attach call %q, got %v", expectedAttach, calls)
	}
	snapshotCount := 0
	infoIdx := -1
	snapshotIdx := -1
	for i, call := range calls {
		if call == "info" {
			infoIdx = i
		}
		if call != "snapshot" {
			continue
		}
		snapshotCount++
		if snapshotIdx == -1 {
			snapshotIdx = i
		}
	}
	if infoIdx == -1 {
		t.Fatalf("expected bootstrap pane metadata lookup, got %v", calls)
	}
	if snapshotCount != 1 {
		t.Fatalf("expected a single resized snapshot capture, got %v", calls)
	}
	if infoIdx > resizeIdx || resizeIdx > snapshotIdx {
		t.Fatalf("expected metadata lookup before resize and final snapshot after resize, got call order %v", calls)
	}
	if snapshotIdx > attachIdx {
		t.Fatalf("expected reattach snapshot before live attach, got call order %v", calls)
	}
	scrollbackIdx := -1
	scrollbackCount := 0
	verifyIdx := -1
	for i, call := range calls {
		if call == "scrollback" {
			scrollbackCount++
			if scrollbackIdx == -1 {
				scrollbackIdx = i
			}
		}
		if call == "verify" {
			verifyIdx = i
		}
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
