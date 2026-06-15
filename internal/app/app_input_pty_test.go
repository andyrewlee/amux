package app

import (
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// newPTYTestCenter builds a real center model wired to a single workspace so
// the App PTY handlers exercise the same delegation path as production without
// needing a live tmux/PTY.
func newPTYTestCenter(t *testing.T, ws *data.Workspace) *center.Model {
	t.Helper()
	cfg := &config.Config{
		Assistants: map[string]config.AssistantConfig{"claude": {}},
	}
	m := center.New(cfg)
	m.SetWorkspace(ws)
	return m
}

func ptyTestWorkspace() *data.Workspace {
	return &data.Workspace{Name: "ws", Repo: "/repo/ws", Root: "/repo/ws"}
}

// --- handlePTYMessages -------------------------------------------------------

func TestHandlePTYMessages_DelegatesAndPreservesModel(t *testing.T) {
	ws := ptyTestWorkspace()
	centerModel := newPTYTestCenter(t, ws)
	app := &App{center: centerModel}

	// A non-"stopped" TabSessionStatus is a pure no-op inside center.Update: it
	// returns the same model and a nil command. This verifies handlePTYMessages
	// forwards the message and writes the returned model back onto the App.
	cmd := app.handlePTYMessages(messages.TabSessionStatus{
		WorkspaceID: string(ws.ID()),
		SessionName: "amux-noop",
		Status:      "running",
	})
	if cmd != nil {
		t.Fatalf("expected nil cmd for no-op status, got %T", cmd())
	}
	if app.center != centerModel {
		t.Fatal("expected App.center to be reassigned to the model returned by Update")
	}
}

func TestHandlePTYMessages_UnknownStoppedSessionIsNoop(t *testing.T) {
	ws := ptyTestWorkspace()
	centerModel := newPTYTestCenter(t, ws)
	app := &App{center: centerModel}

	// "stopped" for a session that has no matching tab returns early (nil cmd)
	// before touching the (nil) agent manager, so this must not panic.
	cmd := app.handlePTYMessages(messages.TabSessionStatus{
		WorkspaceID: string(ws.ID()),
		SessionName: "does-not-exist",
		Status:      "stopped",
	})
	if cmd != nil {
		t.Fatalf("expected nil cmd for unknown stopped session, got %T", cmd())
	}
	if app.center == nil {
		t.Fatal("expected App.center to remain set after delegation")
	}
}

func TestHandlePTYMessages_WorkspaceDeletedCleansUpTabs(t *testing.T) {
	ws := ptyTestWorkspace()
	wsID := string(ws.ID())
	centerModel := newPTYTestCenter(t, ws)
	centerModel.AddTab(&center.Tab{
		ID:        center.TabID("tab-1"),
		Name:      "claude",
		Assistant: "claude",
		Workspace: ws,
		Running:   true,
	})
	if got, _ := centerModel.GetTabsInfoForWorkspace(wsID); len(got) != 1 {
		t.Fatalf("precondition: expected 1 tab before delete, got %d", len(got))
	}

	app := &App{center: centerModel}
	// WorkspaceDeleted routes through center.Update -> CleanupWorkspace, which is
	// an observable state change: the workspace's tabs are dropped. This proves
	// handlePTYMessages forwards the message to the real center model.
	app.handlePTYMessages(messages.WorkspaceDeleted{Workspace: ws})

	if got, _ := centerModel.GetTabsInfoForWorkspace(wsID); len(got) != 0 {
		t.Fatalf("expected tabs to be cleaned up after WorkspaceDeleted, got %d", len(got))
	}
	if app.center != centerModel {
		t.Fatal("expected App.center to be preserved through delegation")
	}
}

// --- handleSidebarPTYMessages ------------------------------------------------

func TestHandleSidebarPTYMessages_DelegatesAndPreservesModel(t *testing.T) {
	term := sidebar.NewTerminalModel()
	app := &App{sidebarTerminal: term}

	// SidebarPTYStopped for a terminal that was never started is a safe no-op;
	// it verifies the message is forwarded and the model is written back.
	cmd := app.handleSidebarPTYMessages(messages.SidebarPTYStopped{
		WorkspaceID: "ws-missing",
		TabID:       "missing-tab",
	})
	if cmd != nil {
		t.Fatalf("expected nil cmd for stopped on missing tab, got %T", cmd())
	}
	if app.sidebarTerminal != term {
		t.Fatal("expected App.sidebarTerminal to be reassigned to the returned model")
	}
}

func TestHandleSidebarPTYMessages_UnhandledMessageReturnsNil(t *testing.T) {
	term := sidebar.NewTerminalModel()
	app := &App{sidebarTerminal: term}

	// A message the sidebar Update switch does not recognize falls through to a
	// nil command without mutating the model identity.
	cmd := app.handleSidebarPTYMessages(struct{ unrelated bool }{unrelated: true})
	if cmd != nil {
		t.Fatalf("expected nil cmd for unhandled sidebar message, got %T", cmd())
	}
	if app.sidebarTerminal != term {
		t.Fatal("expected App.sidebarTerminal to be preserved for unhandled message")
	}
}

// --- handleTabInputFailed ----------------------------------------------------

func TestHandleTabInputFailed_EmptyWorkspaceShowsToastAndSkipsDetach(t *testing.T) {
	ws := ptyTestWorkspace()
	centerModel := newPTYTestCenter(t, ws)
	app := &App{
		center:          centerModel,
		toast:           common.NewToastModel(),
		activeWorkspace: data.NewWorkspace("active", "main", "main", "/repo", "/repo"),
		lifecycle:       newWorkspaceLifecycleState(),
	}

	cmds := app.handleTabInputFailed(center.TabInputFailed{WorkspaceID: ""})

	// With no workspace id there is no detach command, leaving the warning toast
	// plus the active-workspace persist command.
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands (toast + persist), got %d", len(cmds))
	}
	if !app.toast.Visible() {
		t.Fatal("expected a warning toast to be visible")
	}
	if got := app.toast.View(); !strings.Contains(got, "Session disconnected") {
		t.Fatalf("expected disconnect warning in toast view, got %q", got)
	}
}

func TestHandleTabInputFailed_DetachesLiveTab(t *testing.T) {
	ws := ptyTestWorkspace()
	wsID := string(ws.ID())
	centerModel := newPTYTestCenter(t, ws)
	tab := &center.Tab{
		ID:        center.TabID("tab-live"),
		Name:      "claude",
		Assistant: "claude",
		Workspace: ws,
		Running:   true,
	}
	centerModel.AddTab(tab)

	app := &App{
		center:          centerModel,
		toast:           common.NewToastModel(),
		activeWorkspace: ws,
		lifecycle:       newWorkspaceLifecycleState(),
	}

	cmds := app.handleTabInputFailed(center.TabInputFailed{
		WorkspaceID: wsID,
		TabID:       center.TabID("tab-live"),
	})

	// Toast + detach (live tab exists) + persist (active workspace set).
	if len(cmds) != 3 {
		t.Fatalf("expected 3 commands (toast + detach + persist), got %d", len(cmds))
	}
	if !app.toast.Visible() {
		t.Fatal("expected a warning toast to be visible")
	}
}

func TestHandleTabInputFailed_UnknownTabSkipsDetach(t *testing.T) {
	ws := ptyTestWorkspace()
	wsID := string(ws.ID())
	centerModel := newPTYTestCenter(t, ws)

	app := &App{
		center:          centerModel,
		toast:           common.NewToastModel(),
		activeWorkspace: ws,
		lifecycle:       newWorkspaceLifecycleState(),
	}

	// WorkspaceID is set but matches no live tab, so DetachTabByID returns nil
	// and contributes no command; only toast + persist remain.
	cmds := app.handleTabInputFailed(center.TabInputFailed{
		WorkspaceID: wsID,
		TabID:       center.TabID("ghost"),
	})
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands (toast + persist) for unknown tab, got %d", len(cmds))
	}
}

func TestHandleTabInputFailed_NilActiveWorkspaceSkipsPersist(t *testing.T) {
	ws := ptyTestWorkspace()
	centerModel := newPTYTestCenter(t, ws)

	app := &App{
		center:          centerModel,
		toast:           common.NewToastModel(),
		activeWorkspace: nil,
		lifecycle:       newWorkspaceLifecycleState(),
	}

	// No active workspace means persistActiveWorkspaceTabs returns nil; with an
	// empty workspace id there is also no detach, so only the toast remains.
	cmds := app.handleTabInputFailed(center.TabInputFailed{WorkspaceID: ""})
	if len(cmds) != 1 {
		t.Fatalf("expected only the toast command, got %d", len(cmds))
	}
	if !app.toast.Visible() {
		t.Fatal("expected a warning toast to be visible")
	}
}

// --- handleSpinnerTick -------------------------------------------------------

func TestHandleSpinnerTick_NoPendingWorkIsQuiet(t *testing.T) {
	ws := ptyTestWorkspace()
	app := &App{
		center:       newPTYTestCenter(t, ws),
		dashboard:    dashboard.New(),
		tmuxActivity: newTmuxActivityState(),
	}

	// With nothing creating or deleting, the dashboard ignores the tick and
	// StartSpinnerIfNeeded reports nothing to start: no commands are emitted.
	cmds := app.handleSpinnerTick(dashboard.SpinnerTickMsg{})
	if len(cmds) != 0 {
		t.Fatalf("expected no commands when idle, got %d", len(cmds))
	}
}

func TestHandleSpinnerTick_PendingWorkSchedulesNextTick(t *testing.T) {
	ws := ptyTestWorkspace()
	dash := dashboard.New()
	// Marking a workspace as creating arms the spinner; the next tick must be
	// rescheduled by handleSpinnerTick via dashboard.Update.
	creating := data.NewWorkspace("creating", "main", "main", "/repo", "/repo")
	dash.SetWorkspaceCreating(creating, true)

	app := &App{
		center:       newPTYTestCenter(t, ws),
		dashboard:    dash,
		tmuxActivity: newTmuxActivityState(),
	}

	cmds := app.handleSpinnerTick(dashboard.SpinnerTickMsg{})
	if len(cmds) == 0 {
		t.Fatal("expected at least one command while a workspace is creating")
	}

	// The rescheduled command must yield another SpinnerTickMsg.
	var sawTick bool
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		if _, ok := cmd().(dashboard.SpinnerTickMsg); ok {
			sawTick = true
		}
	}
	if !sawTick {
		t.Fatal("expected a follow-up SpinnerTickMsg command to be scheduled")
	}
	if app.dashboard == nil {
		t.Fatal("expected App.dashboard to remain set after Update")
	}
}

// --- handlePTYWatchdogTick ---------------------------------------------------

func TestHandlePTYWatchdogTick_AlwaysReschedules(t *testing.T) {
	ws := ptyTestWorkspace()
	app := &App{
		center:          newPTYTestCenter(t, ws),
		sidebarTerminal: sidebar.NewTerminalModel(),
		dashboard:       dashboard.New(),
		tmuxActivity:    newTmuxActivityState(),
	}

	// With no PTY readers to start, both StartPTYReaders calls return nil, so
	// the only command is the watchdog reschedule. We assert the command is
	// present without invoking it: the SafeTick uses a real 5s timer, and the
	// reschedule's PTYWatchdogTick payload is already covered by the
	// startPTYWatchdog test in app_init_more_test.go.
	cmds := app.handlePTYWatchdogTick()
	if len(cmds) != 1 {
		t.Fatalf("expected exactly the watchdog reschedule command, got %d", len(cmds))
	}
	if cmds[0] == nil {
		t.Fatal("expected a non-nil watchdog command")
	}
}

func TestHandlePTYWatchdogTick_NilComponentsStillReschedule(t *testing.T) {
	// Guard the nil-checks: a partially-initialized App must not panic and must
	// still reschedule the watchdog.
	app := &App{
		dashboard:    dashboard.New(),
		tmuxActivity: newTmuxActivityState(),
	}

	cmds := app.handlePTYWatchdogTick()
	if len(cmds) != 1 {
		t.Fatalf("expected only the watchdog reschedule with nil panes, got %d", len(cmds))
	}
	if cmds[0] == nil {
		t.Fatal("expected a non-nil watchdog command even with nil panes")
	}
}
