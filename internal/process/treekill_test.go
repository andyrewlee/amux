//go:build !windows

package process

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestKillProcessGroup_BasicTermination(t *testing.T) {
	// Start a process that responds to SIGTERM
	cmd := exec.Command("sh", "-c", "sleep 60")
	SetProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	pid := cmd.Process.Pid

	// Give the process group time to be established
	time.Sleep(20 * time.Millisecond)

	// Kill the process group
	err := KillProcessGroup(pid, KillOptions{GracePeriod: 100 * time.Millisecond})
	if err != nil {
		if err == syscall.EPERM {
			t.Skip("signal permissions restricted in this environment")
		}
		t.Errorf("KillProcessGroup returned error: %v", err)
	}

	// Wait for the process to exit
	_ = cmd.Wait()

	// Verify process is gone
	err = syscall.Kill(pid, 0)
	if err != syscall.ESRCH {
		t.Errorf("process still running after kill")
	}
}

func TestKillProcessGroup_EscalationToSIGKILL(t *testing.T) {
	// Start a process that ignores SIGTERM
	cmd := exec.Command("sh", "-c", "trap '' TERM; sleep 60")
	SetProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	pid := cmd.Process.Pid

	// Give the process group time to be established
	time.Sleep(20 * time.Millisecond)

	// Kill with short grace period - should escalate to SIGKILL
	start := time.Now()
	err := KillProcessGroup(pid, KillOptions{GracePeriod: 50 * time.Millisecond})
	elapsed := time.Since(start)

	if err != nil {
		if err == syscall.EPERM {
			t.Skip("signal permissions restricted in this environment")
		}
		t.Errorf("KillProcessGroup returned error: %v", err)
	}

	// Should have waited at least the grace period
	if elapsed < 50*time.Millisecond {
		t.Errorf("returned too quickly: %v", elapsed)
	}

	// Wait for the process to exit
	_ = cmd.Wait()

	// Verify process is gone
	err = syscall.Kill(pid, 0)
	if err != syscall.ESRCH {
		t.Errorf("process still running after SIGKILL")
	}
}

func TestKillProcessGroup_AlreadyExited(t *testing.T) {
	// Start and immediately finish a process
	cmd := exec.Command("sh", "-c", "exit 0")
	SetProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	pid := cmd.Process.Pid

	// Wait for it to exit naturally
	_ = cmd.Wait()

	// Killing an already-exited process should not error
	err := KillProcessGroup(pid, KillOptions{})
	if err != nil {
		t.Errorf("KillProcessGroup returned error for already-exited process: %v", err)
	}
}

func TestKillProcessGroup_ChildProcessCleanup(t *testing.T) {
	// Start a parent that spawns children
	cmd := exec.Command("sh", "-c", "sleep 60 & sleep 60 & wait")
	SetProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	pid := cmd.Process.Pid

	// Give children time to spawn
	time.Sleep(50 * time.Millisecond)

	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("failed to get pgid: %v", err)
	}

	// Kill the process group
	err = KillProcessGroup(pid, KillOptions{GracePeriod: 100 * time.Millisecond})
	if err != nil {
		if err == syscall.EPERM {
			t.Skip("signal permissions restricted in this environment")
		}
		t.Errorf("KillProcessGroup returned error: %v", err)
	}

	// Wait for the parent
	_ = cmd.Wait()

	// Verify parent is gone
	err = syscall.Kill(pid, 0)
	if err != syscall.ESRCH {
		t.Errorf("parent process still running")
	}

	// Verify the process group is gone (children cleaned up), with retries for slow cleanup.
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		err = syscall.Kill(-pgid, 0)
		if err == syscall.ESRCH {
			break
		}
		if time.Now().After(deadline) {
			t.Errorf("process group still running")
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSetProcessGroup(t *testing.T) {
	cmd := exec.Command("echo", "test")

	// Initially nil
	if cmd.SysProcAttr != nil {
		t.Error("SysProcAttr should initially be nil")
	}

	SetProcessGroup(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr should not be nil after SetProcessGroup")
	}

	if !cmd.SysProcAttr.Setpgid {
		t.Error("Setpgid should be true")
	}
}

func TestSetProcessGroup_PreserveExisting(t *testing.T) {
	cmd := exec.Command("echo", "test")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(os.Getuid())},
	}

	SetProcessGroup(cmd)

	if !cmd.SysProcAttr.Setpgid {
		t.Error("Setpgid should be true")
	}

	if cmd.SysProcAttr.Credential == nil {
		t.Error("existing Credential should be preserved")
	}
}
