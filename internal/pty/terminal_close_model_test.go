package pty

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/process"
)

// walkCloseTable steps the compiled escalation table through events and
// fails on any rejected move — the differential twin must never leave the
// verified protocol.
func walkCloseTable(t *testing.T, from closeState, events ...closeEvent) closeState {
	t.Helper()
	cur := from
	for _, e := range events {
		next, ok := closeMachine.Next(cur, e)
		if !ok {
			t.Fatalf("close table rejected %v --%v-->", cur, e)
		}
		cur = next
	}
	return cur
}

// assertCloseReaderContract checks the reader surface (IsClosed, Running,
// File) against what the derived machine state promises.
func assertCloseReaderContract(t *testing.T, term *Terminal, state closeState, wantFile bool) {
	t.Helper()
	if got := term.IsClosed(); got != (closeRank(state) >= 2) {
		t.Errorf("IsClosed() = %v in state %v", got, state)
	}
	if got := term.Running(); got != (state == openRunning) {
		t.Errorf("Running() = %v in state %v", got, state)
	}
	if got := term.File(); (got != nil) != wantFile {
		t.Errorf("File() non-nil = %v in state %v, want %v", got != nil, state, wantFile)
	}
}

// TestCloseModelCheck runs the formal verification of the escalation table.
func TestCloseModelCheck(t *testing.T) {
	if err := CloseModelCheck(); err != nil {
		t.Fatalf("CloseModelCheck: %v", err)
	}
}

// TestCloseModelDifferential_NaturalExitThenClose: the process exits on its
// own; the monitor completes; Close fast-paths with no signal at all.
func TestCloseModelDifferential_NaturalExitThenClose(t *testing.T) {
	term, err := New("true", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s := term.snapshotClose(phaseOpen).state(); s != openRunning {
		t.Fatalf("initial state = %v, want %v", s, openRunning)
	}
	assertCloseReaderContract(t, term, openRunning, true)

	waitUntilTerminal(t, 2*time.Second, func() bool { return !term.Running() })
	got := term.snapshotClose(phaseOpen).state()
	if got != openExited {
		t.Fatalf("after natural exit state = %v, want %v", got, openExited)
	}
	assertCloseReaderContract(t, term, openExited, true)

	// No kill may fire: the monitor already reaped the process.
	restoreTerminalHooks(t)
	terminalKillProcessGroup = func(int, process.KillOptions) error {
		t.Error("Close signaled a process the monitor already reaped")
		return nil
	}

	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	end := term.snapshotClose(phaseSettled).state()
	if end != closedReaped {
		t.Fatalf("end state = %v, want %v", end, closedReaped)
	}
	want := walkCloseTable(t, openRunning, evtProcExit, evtCloseBegin)
	if end != want {
		t.Fatalf("differential: derived %v, table %v", end, want)
	}
	assertCloseReaderContract(t, term, closedReaped, false)
}

// TestCloseModelDifferential_SigtermPath: a live process dies within the
// SIGTERM budget; Close reaps without escalating.
func TestCloseModelDifferential_SigtermPath(t *testing.T) {
	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s := term.snapshotClose(phaseOpen).state(); s != openRunning {
		t.Fatalf("initial state = %v, want %v", s, openRunning)
	}

	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	end := term.snapshotClose(phaseSettled).state()
	if end != closedReaped {
		t.Fatalf("end state = %v, want %v", end, closedReaped)
	}
	want := walkCloseTable(t, openRunning, evtCloseBegin, evtSigtermWaitOk)
	if end != want {
		t.Fatalf("differential: derived %v, table %v", end, want)
	}
	assertCloseReaderContract(t, term, closedReaped, false)
}

// TestCloseModelDifferential_SigtermTimeoutEscalates: when the process
// survives SIGTERM (kill hook no-op stands in for a HUP/TERM-ignoring
// process) and SIGKILL reaps it, Close walks the timeout rung. Note the
// real KillProcessGroup escalates internally on its own 200ms grace, so
// with real signals this outer rung is a backstop for unkillable or
// stuck-waiter cases, not the normal path — hence the seam.
func TestCloseModelDifferential_SigtermTimeoutEscalates(t *testing.T) {
	restoreTerminalHooks(t)
	terminalCloseTimeout = 50 * time.Millisecond
	terminalKillProcessGroup = func(int, process.KillOptions) error { return nil }

	waitDone := make(chan struct{})
	forceKilled := make(chan int, 1)
	terminalForceKillProcess = func(pid int) error {
		forceKilled <- pid
		close(waitDone) // SIGKILL reaps: the wait monitor completes
		return nil
	}

	const pid = 99_999_995
	term := &Terminal{
		cmd:      &exec.Cmd{Process: &os.Process{Pid: pid}},
		waitDone: waitDone,
	}
	if s := term.snapshotClose(phaseOpen).state(); s != openRunning {
		t.Fatalf("initial state = %v, want %v", s, openRunning)
	}

	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case got := <-forceKilled:
		if got != pid {
			t.Fatalf("ForceKillProcess pid = %d, want %d", got, pid)
		}
	default:
		t.Fatal("Close never escalated to SIGKILL")
	}
	end := term.snapshotClose(phaseSettled).state()
	if end != closedReaped {
		t.Fatalf("end state = %v, want %v", end, closedReaped)
	}
	want := walkCloseTable(t, openRunning,
		evtCloseBegin, evtSigtermTimeout, evtSigkillOk)
	if end != want {
		t.Fatalf("differential: derived %v, table %v", end, want)
	}
	assertCloseReaderContract(t, term, closedReaped, false)
}

// TestCloseModelDifferential_SigkillTimeoutAbandons: even SIGKILL produces
// no wait (the waiter is stuck); Close abandons and returns, and the stuck
// channel stays open — the only field-level witness of abandonment. (For
// monitor-less terminals the fresh wait channel Close installs is a local,
// so abandonment there is field-invisible; this case uses a pre-set
// waitDone, which Close reuses.)
func TestCloseModelDifferential_SigkillTimeoutAbandons(t *testing.T) {
	restoreTerminalHooks(t)
	terminalCloseTimeout = 20 * time.Millisecond
	terminalKillProcessGroup = func(int, process.KillOptions) error { return nil }
	terminalForceKillProcess = func(int) error { return nil }

	stuckWait := make(chan struct{}) // never closed: the waiter hangs
	term := &Terminal{
		cmd:      &exec.Cmd{Process: &os.Process{Pid: 99_999_996}},
		waitDone: stuckWait,
	}
	if s := term.snapshotClose(phaseOpen).state(); s != openRunning {
		t.Fatalf("initial state = %v, want %v", s, openRunning)
	}

	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	end := term.snapshotClose(phaseSettled).state()
	if end != closedAbandoned {
		t.Fatalf("end state = %v, want %v", end, closedAbandoned)
	}
	want := walkCloseTable(t, openRunning,
		evtCloseBegin, evtSigtermTimeout, evtSigkillTimeout)
	if end != want {
		t.Fatalf("differential: derived %v, table %v", end, want)
	}
	assertCloseReaderContract(t, term, closedAbandoned, false)
}

// TestCloseModelDifferential_DoubleCloseIdempotent: from every resting
// closed state a second Close changes nothing observable, matching the
// table's no-op self-loops.
func TestCloseModelDifferential_DoubleCloseIdempotent(t *testing.T) {
	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	before := term.snapshotClose(phaseSettled).state()
	if before != closedReaped {
		t.Fatalf("state after first Close = %v, want %v", before, closedReaped)
	}
	if err := term.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	after := term.snapshotClose(phaseSettled).state()
	if after != before {
		t.Fatalf("second Close moved %v -> %v; must be a no-op self-loop", before, after)
	}
	if want := walkCloseTable(t, closedReaped, evtCloseBegin); after != want {
		t.Fatalf("differential: derived %v, table %v", after, want)
	}
	assertCloseReaderContract(t, term, closedReaped, false)
}

// TestCloseModelDifferential_MonitorLessTerminal pins the once-observed
// quirk and the monitor-less variant end to end: Running() falls back to
// cmd.ProcessState when waitDone is nil (running while nil, stopped once an
// external waiter records the exit), and Close gives such a terminal a
// fresh async wait before signaling.
func TestCloseModelDifferential_MonitorLessTerminal(t *testing.T) {
	restoreTerminalHooks(t)
	terminalCloseTimeout = time.Second

	cmd := exec.Command("sh", "-c", "true")
	cmd.Dir = t.TempDir()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	term := &Terminal{cmd: cmd}

	// Quirk pin: no monitor, ProcessState nil -> Running() reports true via
	// the fallback branch.
	if s := term.snapshotClose(phaseOpen).state(); s != openRunning {
		t.Fatalf("monitor-less initial state = %v, want %v", s, openRunning)
	}
	if !term.Running() {
		t.Fatal("Running() = false for monitor-less live terminal (ProcessState fallback broken)")
	}

	// External wait flips the fallback: still no monitor, but reaped.
	if err := cmd.Wait(); err != nil {
		t.Fatalf("external wait: %v", err)
	}
	got := term.snapshotClose(phaseOpen).state()
	if got != openExitedNoMonitor {
		t.Fatalf("after external wait state = %v, want %v", got, openExitedNoMonitor)
	}
	// Quirk pin: Running() consults ProcessState because waitDone is nil.
	if term.Running() {
		t.Fatal("Running() = true after external wait recorded ProcessState")
	}

	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	end := term.snapshotClose(phaseSettled).state()
	if end != closedReaped {
		t.Fatalf("end state = %v, want %v", end, closedReaped)
	}
	// Close attaches the fresh async wait (monitor-attach from a
	// waitDone-nil state), then the ladder reaps immediately.
	want := walkCloseTable(t, openRunning, evtExternalWait,
		evtMonitorAttach, evtCloseBegin)
	if end != want {
		t.Fatalf("differential: derived %v, table %v", end, want)
	}
	assertCloseReaderContract(t, term, closedReaped, false)
}
