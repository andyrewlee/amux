package pty

import (
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/process"
)

func restoreTerminalHooks(t *testing.T) {
	t.Helper()
	prevTimeout := terminalCloseTimeout
	prevKillProcessGroup := terminalKillProcessGroup
	prevForceKillProcess := terminalForceKillProcess
	prevWaitCommand := terminalWaitCommand
	t.Cleanup(func() {
		terminalCloseTimeout = prevTimeout
		terminalKillProcessGroup = prevKillProcessGroup
		terminalForceKillProcess = prevForceKillProcess
		terminalWaitCommand = prevWaitCommand
	})
}

func waitUntilTerminal(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if cond() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("condition did not become true before timeout")
		case <-ticker.C:
		}
	}
}

func TestTerminal_RunningFalseAfterNaturalExitBeforeClose(t *testing.T) {
	term, err := New("true", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer term.Close()

	waitUntilTerminal(t, 2*time.Second, func() bool {
		return !term.Running()
	})
}

func TestTerminal_CloseSkipsWaitWhenMonitorAlreadyComplete(t *testing.T) {
	restoreTerminalHooks(t)

	var waitCalls atomic.Int32
	terminalWaitCommand = func(*exec.Cmd) error {
		waitCalls.Add(1)
		return nil
	}
	terminalKillProcessGroup = func(int, process.KillOptions) error {
		t.Fatal("Close attempted to kill after the wait monitor completed")
		return nil
	}

	waitDone := make(chan struct{})
	close(waitDone)
	term := &Terminal{
		cmd:      &exec.Cmd{Process: &os.Process{Pid: 99_999_998}},
		waitDone: waitDone,
	}

	if err := term.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if got := waitCalls.Load(); got != 0 {
		t.Fatalf("terminalWaitCommand calls = %d, want 0", got)
	}
}

func TestTerminal_CloseUsesExistingWaitMonitor(t *testing.T) {
	restoreTerminalHooks(t)

	terminalCloseTimeout = 10 * time.Millisecond
	var waitCalls atomic.Int32
	terminalWaitCommand = func(*exec.Cmd) error {
		waitCalls.Add(1)
		return nil
	}
	terminalKillProcessGroup = func(int, process.KillOptions) error { return nil }

	waitDone := make(chan struct{})
	forceKilled := make(chan int, 1)
	terminalForceKillProcess = func(pid int) error {
		forceKilled <- pid
		close(waitDone)
		return nil
	}

	const pid = 99_999_997
	term := &Terminal{
		cmd:      &exec.Cmd{Process: &os.Process{Pid: pid}},
		waitDone: waitDone,
	}

	if err := term.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if got := waitCalls.Load(); got != 0 {
		t.Fatalf("terminalWaitCommand calls = %d, want 0", got)
	}
	select {
	case got := <-forceKilled:
		if got != pid {
			t.Fatalf("ForceKillProcess pid = %d, want %d", got, pid)
		}
	default:
		t.Fatal("expected Close to escalate to ForceKillProcess")
	}
}
