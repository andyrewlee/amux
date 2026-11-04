package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// TestRunOutputFlavorMachine verifies the shared OutputDialog slot's flavor
// machine and the begin/close choke points that make cross-flavor flag
// leaks unwritable.
func TestRunOutputFlavorMachine(t *testing.T) {
	if err := runOutputFlavorCheck(); err != nil {
		t.Fatalf("run-output flavor machine failed model checking: %v", err)
	}
	// Every open flavor can replace every other (total opens) and close
	// returns to closed — spot-check the shape the checker proves exhaustively.
	for _, from := range []runOutputFlavor{
		runOutputClosed, runOutputFlavorRun, runOutputFlavorStatus,
		runOutputFlavorScript, runOutputFlavorEvents,
	} {
		for _, open := range []runOutputFlavorEvent{
			runOutputEvtOpenRun, runOutputEvtOpenStatus, runOutputEvtOpenScript, runOutputEvtOpenEvents,
		} {
			if to, ok := runOutputFlavorMachine.Next(from, open); !ok || to == runOutputClosed {
				t.Errorf("open %v from %v rejected or closed: ok=%v to=%v", open, from, ok, to)
			}
		}
		if to, ok := runOutputFlavorMachine.Next(from, runOutputEvtClose); !ok || to != runOutputClosed {
			t.Errorf("close from %v: ok=%v to=%v, want closed", from, ok, to)
		}
	}
}

// TestRunOutputFlavorChokePointResetsCrossFlavorData proves the open
// choke point leaves no flavor-scoped data behind: opening the status
// flavor over a live run viewer disarms attach (session pin, count, untrust
// all reset), and closing clears everything.
func TestRunOutputFlavorChokePointResetsCrossFlavorData(t *testing.T) {
	o := overlayState{
		runOutputFlavor:    runOutputFlavorRun,
		runOutputWorkspace: &data.Workspace{Name: "ws"},
		runOutputSession:   "amux-ws-run",
	}
	o.beginRunOutputFlavor(runOutputFlavorStatus)
	if o.runOutputFlavor != runOutputFlavorStatus {
		t.Fatalf("flavor = %s, want status", o.runOutputFlavor)
	}
	if o.runOutputWorkspace != nil || o.runOutputSession != "" ||
		o.runOutputReleaseCount != 0 || o.runOutputUntrustable || o.runOutput != nil {
		t.Fatalf("status open inherited run-flavor data: %+v", o)
	}
	o.runOutputReleaseCount = 3
	o.runOutputUntrustable = true
	o.closeRunOutputFlavor()
	if o.runOutputFlavor != runOutputClosed || o.runOutputReleaseCount != 0 || o.runOutputUntrustable {
		t.Fatalf("close left flavor-scoped data behind: %+v", o)
	}
}

// TestRunOutputFlavorInterceptsFollowEnum drives the hot key-intercept
// gates through the flavor enum: `a` answers only the run flavor, `R` only
// the status flavor with a non-zero count, `U` only the status flavor with
// the untrust arm.
func TestRunOutputFlavorInterceptsFollowEnum(t *testing.T) {
	newViewer := func(t *testing.T, flavor runOutputFlavor) *App {
		t.Helper()
		a := &App{toast: common.NewToastModel(), width: 120, height: 40}
		a.overlays.runOutput = common.NewOutputDialog("viewer", "content")
		a.overlays.runOutput.SetSize(120, 40)
		a.overlays.runOutput.Show()
		a.overlays.runOutputFlavor = flavor
		return a
	}
	keyA := func() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'a', Text: "a"} }
	keyR := func() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'R', Text: "R"} }
	keyU := func() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'U', Text: "U"} }

	// `a` on the status flavor must not enqueue an attach — the dialog may
	// still consume the key itself (its own browse-mode handling), but the
	// app-level intercept must stay disarmed.
	a := newViewer(t, runOutputFlavorStatus)
	var cmds []tea.Cmd
	a.handleRunOutputInput(keyA(), &cmds)
	if len(cmds) != 0 {
		t.Fatal("status flavor armed `a` — attach leaked into a non-run viewer")
	}
	// `R` on the run flavor must not enqueue a release even with a stale count.
	a = newViewer(t, runOutputFlavorRun)
	a.overlays.runOutputReleaseCount = 2
	cmds = nil
	a.handleRunOutputInput(keyR(), &cmds)
	if a.dialog != nil {
		t.Fatal("run flavor armed `R` — release dialog leaked into the run viewer")
	}
	// `U` on the run flavor must not open the revoke dialog even with the
	// arm set.
	a = newViewer(t, runOutputFlavorRun)
	a.overlays.runOutputUntrustable = true
	cmds = nil
	a.handleRunOutputInput(keyU(), &cmds)
	if a.dialog != nil {
		t.Fatal("run flavor armed `U` — revoke dialog leaked into the run viewer")
	}
	// The run flavor still owns `a` (an attach cmd is enqueued).
	a = newViewer(t, runOutputFlavorRun)
	a.overlays.runOutputWorkspace = &data.Workspace{Name: "ws"}
	a.overlays.runOutputSession = "amux-ws-run"
	cmds = nil
	if !a.handleRunOutputInput(keyA(), &cmds) {
		t.Fatal("run flavor must consume `a`")
	}
	if len(cmds) != 1 {
		t.Fatalf("run flavor `a` enqueued %d cmds, want the attach cmd", len(cmds))
	}
}

// TestDialogHandoffMachine verifies the staged-handoff table and its
// fail-closed rejections (cross-staging and re-staging).
func TestDialogHandoffMachine(t *testing.T) {
	if err := dialogHandoffCheck(); err != nil {
		t.Fatalf("dialog handoff machine failed model checking: %v", err)
	}
	cases := []struct {
		name  string
		from  dialogHandoffState
		event dialogHandoffEvent
		want  bool
	}{
		{"none->stageCreate", handoffNone, handoffEvtStageCreate, true},
		{"none->stageLaunch", handoffNone, handoffEvtStageLaunch, true},
		{"create->consume", handoffCreateStaged, handoffEvtConsumeCreate, true},
		{"launch->consume", handoffLaunchStaged, handoffEvtConsumeLaunch, true},
		{"create re-stage rejected", handoffCreateStaged, handoffEvtStageCreate, false},
		{"launch re-stage rejected", handoffLaunchStaged, handoffEvtStageLaunch, false},
		{"create->stageLaunch rejected", handoffCreateStaged, handoffEvtStageLaunch, false},
		{"launch->stageCreate rejected", handoffLaunchStaged, handoffEvtStageCreate, false},
		{"consume from none rejected", handoffNone, handoffEvtConsumeCreate, false},
	}
	for _, tc := range cases {
		if _, ok := dialogHandoffMachine.Next(tc.from, tc.event); ok != tc.want {
			t.Errorf("%s: Next(%s, %s) ok = %v, want %v", tc.name, tc.from, tc.event, ok, tc.want)
		}
	}
}

// TestDialogHandoffStageRejectsCrossStaging proves the runtime staging
// helper refuses to write the data slot when the table rejects — the
// stale-binding hazard (an agent pick silently creating a workspace) is
// unwritable.
func TestDialogHandoffStageRejectsCrossStaging(t *testing.T) {
	a := &App{}
	a.stagePendingLaunch("claude", &data.Workspace{Name: "ws"})
	if a.dialogHandoff != handoffLaunchStaged || a.pendingLaunchTask.assistant != "claude" {
		t.Fatalf("launch staging failed: handoff=%s pending=%+v", a.dialogHandoff, a.pendingLaunchTask)
	}
	proj := &data.Project{Name: "alpha"}
	a.stagePendingWorkspaceCreate(proj, "ws-new", "")
	if a.dialogHandoff != handoffLaunchStaged {
		t.Fatal("create staging over an armed launch was accepted")
	}
	if a.pendingWorkspaceCreate.project != nil {
		t.Fatal("rejected staging still wrote the create slot — stale binding armed")
	}
	// Consume with nothing staged warns but never fabricates data.
	a2 := &App{}
	if got := a2.consumePendingWorkspaceCreate(); got.project != nil || got.name != "" {
		t.Fatalf("consume from none fabricated %+v", got)
	}
}

// TestDialogSlotMachine verifies the dialog open/replace/stale-drop table.
func TestDialogSlotMachine(t *testing.T) {
	if err := dialogSlotCheck(); err != nil {
		t.Fatalf("dialog slot machine failed model checking: %v", err)
	}
}

// TestOverlayArbiterMachine verifies the deferred-open arbitration table,
// including the drop-on-full edge and the runOutput slot-refresh
// self-deadlock avoidance.
func TestOverlayArbiterMachine(t *testing.T) {
	if err := overlayArbiterCheck(); err != nil {
		t.Fatalf("overlay arbiter machine failed model checking: %v", err)
	}
	// The slot-refresh edge exists from every state and never moves.
	for _, from := range []overlayArbiterState{arbFreeHidden, arbFreeShown, arbQueuedShown, arbQueuedHidden} {
		to, ok := overlayArbiterMachine.Next(from, arbEvtSlotRefresh)
		if !ok || to != from {
			t.Errorf("slot refresh from %s: ok=%v to=%s — must be a self-loop", from, ok, to)
		}
	}
	// Overflow requires a full queue (queued-shown).
	for _, from := range []overlayArbiterState{arbFreeHidden, arbFreeShown, arbQueuedHidden} {
		if _, ok := overlayArbiterMachine.Next(from, arbEvtOverflowDrop); ok {
			t.Errorf("overflow declared from %s — only a full (queued-shown) queue can drop", from)
		}
	}
}

// TestScanRoleMachine verifies the shared-scan ownership pipeline: only
// owners publish, demotion is one-way per scan, and the claim path always
// resolves.
func TestScanRoleMachine(t *testing.T) {
	if err := scanRoleCheck(); err != nil {
		t.Fatalf("scan role machine failed model checking: %v", err)
	}
}

// TestStartupPhaseMachine verifies the startup sequencing gate: all three
// arrival orders reach running, and running is one-way.
func TestStartupPhaseMachine(t *testing.T) {
	if err := startupCheck(); err != nil {
		t.Fatalf("startup machine failed model checking: %v", err)
	}
	if err := activitySettleCheck(); err != nil {
		t.Fatalf("activity settle machine failed model checking: %v", err)
	}
}

// TestPersistDebounceMachine verifies the tab-persist debounce loop: a
// failed save re-dirties, and nothing cleans a slot without a save result.
func TestPersistDebounceMachine(t *testing.T) {
	if err := persistSlotCheck(); err != nil {
		t.Fatalf("persist debounce machine failed model checking: %v", err)
	}
}

// TestSettingsObligationMachine verifies the settings save-obligation and
// theme-preview tables: failures stay owed, only success discharges, and
// Esc restores the open-time original.
func TestSettingsObligationMachine(t *testing.T) {
	if err := settingsObligationCheck(); err != nil {
		t.Fatalf("settings obligation machine failed model checking: %v", err)
	}
	if err := themePreviewCheck(); err != nil {
		t.Fatalf("theme preview machine failed model checking: %v", err)
	}
}

// TestUpdateFlowMachine verifies the update-prompt flow: upgrades start
// only from available, only success clears the prompt, failures retry.
func TestUpdateFlowMachine(t *testing.T) {
	if err := updateFlowCheck(); err != nil {
		t.Fatalf("update flow machine failed model checking: %v", err)
	}
}

// TestRunScriptStatusMachine verifies the crashed-run toast edge and the
// status-check single-flight.
func TestRunScriptStatusMachine(t *testing.T) {
	if err := runTrackCheck(); err != nil {
		t.Fatalf("run toast machine failed model checking: %v", err)
	}
	if err := runCheckCheck(); err != nil {
		t.Fatalf("run status single-flight machine failed model checking: %v", err)
	}
}

// TestStateWatcherDebounceMachine verifies the watcher debounce lifecycle:
// close drops queued paths and is terminal.
func TestStateWatcherDebounceMachine(t *testing.T) {
	if err := watcherDebounceCheck(); err != nil {
		t.Fatalf("state watcher machine failed model checking: %v", err)
	}
}

// TestPrefixModeMachine verifies the prefix-mode exit discipline.
func TestPrefixModeMachine(t *testing.T) {
	if err := prefixModeCheck(); err != nil {
		t.Fatalf("prefix mode machine failed model checking: %v", err)
	}
}
