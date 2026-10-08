package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
	"github.com/andyrewlee/amux/internal/update"
)

// panicUpdateService panics on every call — the upgrade producer's
// deterministic panic seam.
type panicUpdateService struct{}

func (panicUpdateService) Check() (*update.CheckResult, error) { panic("injected panic") }
func (panicUpdateService) Upgrade(*update.Release) error       { panic("injected panic") }
func (panicUpdateService) IsHomebrewBuild() bool               { panic("injected panic") }

// panicTmuxOps embeds a nil TmuxOps: any method call dispatches on the nil
// interface and panics — a panic seam for the activity-scan producer without
// stubbing the interface's ~20 methods.
type panicTmuxOps struct{ TmuxOps }

// TestGitStatusCmd_PanicEmitsTrackedResult proves a panic inside the
// single-root status producer still emits a Tracked GitStatusResult — the
// result handler clears the root's in-flight mark, so the column keeps
// refreshing instead of freezing for the session.
func TestGitStatusCmd_PanicEmitsTrackedResult(t *testing.T) {
	a := &App{gitStatus: panicGitStatus{}}
	a.initGitStatusDedup()

	cmd := a.enqueueGitStatus("/repo", false)
	if cmd == nil {
		t.Fatal("expected the status cmd")
	}
	if !a.gitStatusInFlight["/repo"] {
		t.Fatal("dispatch did not mark the root in-flight")
	}

	res, ok := cmd().(messages.GitStatusResult)
	if !ok {
		t.Fatalf("panicking producer emitted %T, want messages.GitStatusResult", cmd())
	}
	if res.Err == nil || !res.Tracked || res.Root != "/repo" {
		t.Fatalf("fallback result malformed: %+v", res)
	}

	a.Update(res)
	if a.gitStatusInFlight["/repo"] {
		t.Fatal("in-flight mark not cleared by the panic result")
	}
}

// TestGitStatusBatch_PanicClearsAllMarks proves a mid-batch panic emits one
// failure entry per marked root — handleGitStatusBatchResult clears in-flight
// marks per entry, so a generic messages.Error would strand them all.
func TestGitStatusBatch_PanicClearsAllMarks(t *testing.T) {
	a := &App{gitStatus: panicGitStatus{}, dashboard: dashboard.New()}

	cmd := a.requestGitStatusBatch([]string{"/a", "/b"})
	if cmd == nil {
		t.Fatal("expected the batch cmd")
	}
	if !a.gitStatusInFlight["/a"] || !a.gitStatusInFlight["/b"] {
		t.Fatal("dispatch did not mark the roots in-flight")
	}

	res, ok := cmd().(messages.GitStatusBatchResult)
	if !ok {
		t.Fatalf("panicking producer emitted %T, want messages.GitStatusBatchResult", cmd())
	}
	if len(res.Results) != 2 {
		t.Fatalf("fallback batch carried %d results, want one per marked root", len(res.Results))
	}

	a.Update(res)
	if a.gitStatusInFlight["/a"] || a.gitStatusInFlight["/b"] {
		t.Fatalf("in-flight marks not cleared: %v", a.gitStatusInFlight)
	}
}

// TestUpgradeCmd_PanicClearsRunning proves a panic inside the check/upgrade
// producer emits UpgradeComplete — the only message that clears
// upgradeRunning.
func TestUpgradeCmd_PanicClearsRunning(t *testing.T) {
	a := &App{
		updateService:   panicUpdateService{},
		updateAvailable: &update.CheckResult{},
	}

	cmd := a.handleTriggerUpgrade()
	if cmd == nil {
		t.Fatal("expected the upgrade cmd")
	}
	if !a.upgradeRunning {
		t.Fatal("dispatch did not set upgradeRunning")
	}

	var res messages.UpgradeComplete
	var found bool
	for _, m := range runCommandMessages(cmd) {
		if r, ok := m.(messages.UpgradeComplete); ok {
			res, found = r, true
		}
	}
	if !found {
		t.Fatal("panicking upgrade producer emitted no UpgradeComplete")
	}
	if res.Err == nil {
		t.Fatal("expected the panic error on the result")
	}

	a.Update(res)
	if a.upgradeRunning {
		t.Fatal("upgradeRunning not cleared by the panic result")
	}
}

// TestTmuxActivityScan_PanicClearsScanInFlight proves a panic inside the scan
// emits a tmuxActivityResult carrying the live token — the result handler
// clears scanInFlight on token match, so scanning survives the panic.
func TestTmuxActivityScan_PanicClearsScanInFlight(t *testing.T) {
	a := &App{tmuxAvailable: true, tmuxService: panicTmuxOps{}}

	cmds := a.handleTmuxActivityTick(tmuxActivityTick{Token: a.tmuxActivity.token})
	if len(cmds) != 2 {
		t.Fatalf("expected tick re-arm + scan cmd, got %d", len(cmds))
	}
	if !a.tmuxActivity.scanInFlight {
		t.Fatal("dispatch did not set scanInFlight")
	}

	res, ok := cmds[1]().(tmuxActivityResult)
	if !ok {
		t.Fatalf("panicking scan emitted %T, want tmuxActivityResult", cmds[1]())
	}
	if res.Err == nil || res.Token != a.tmuxActivity.token {
		t.Fatalf("fallback result malformed: %+v (token %v, want %v)", res.Err, res.Token, a.tmuxActivity.token)
	}

	a.Update(res)
	if a.tmuxActivity.scanInFlight {
		t.Fatal("scanInFlight not cleared by the panic result")
	}
}

// TestRunScriptStatus_ResultClearsGuard is the contract the wrapper relies on:
// the typed result clears runScriptStatusInFlight before any staleness check —
// including the LastExit=-1 "unknown" shape the panic fallback emits.
func TestRunScriptStatus_ResultClearsGuard(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	a := &App{
		sidebar:          sidebar.NewTabbedSidebar(),
		activeWorkspace:  ws,
		workspaceService: workspacesvc.New(nil, nil, process.NewScriptRunner(6200, 10), ""),
	}

	cmd := a.requestRunScriptStatus()
	if cmd == nil {
		t.Fatal("expected the status cmd")
	}
	if !a.runScriptStatusInFlight {
		t.Fatal("dispatch did not set the in-flight guard")
	}

	res, ok := cmd().(messages.RunScriptStatusResult)
	if !ok {
		t.Fatalf("status cmd emitted %T, want messages.RunScriptStatusResult", cmd())
	}
	a.Update(res)
	if a.runScriptStatusInFlight {
		t.Fatal("guard not cleared by the typed result")
	}
}

// TestPanicAsMsg_ConvertsPanicToFallback pins the shared mechanism: a panicking
// cmd yields the typed fallback, not messages.Error, and the panic value
// reaches the fallback as an error.
func TestPanicAsMsg_ConvertsPanicToFallback(t *testing.T) {
	cmd := panicAsMsg(func() tea.Msg {
		panic("boom")
	}, func(err error) tea.Msg {
		return messages.RunScriptStatusResult{LastExit: -1, WorkspaceIDs: []string{err.Error()}}
	})
	res, ok := cmd().(messages.RunScriptStatusResult)
	if !ok {
		t.Fatalf("panicAsMsg emitted %T, want the typed fallback", cmd())
	}
	if len(res.WorkspaceIDs) != 1 || res.WorkspaceIDs[0] != "command panic: boom" {
		t.Fatalf("panic value did not reach the fallback: %+v", res)
	}
	if cmd := panicAsMsg(nil, nil); cmd != nil {
		t.Fatal("nil cmd must stay nil")
	}
}
