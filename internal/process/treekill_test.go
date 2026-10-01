//go:build !windows

package process

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/testutil"
)

// pgidEstablished polls until the process group rooted at pid has a member —
// cmd.Start returns before the child has exec'd into the group, and signaling
// a not-yet-populated group silently does nothing.
func pgidEstablished(pid int) bool {
	return syscall.Kill(-pid, 0) == nil
}

// groupMembers returns the live member count of the process group — used to
// wait for forked children to join before asserting a group kill reaches them.
func groupMembers(t *testing.T, pgid int) int {
	t.Helper()
	out, err := exec.Command("pgrep", "-g", strconv.Itoa(pgid)).Output()
	if err != nil {
		return 0
	}
	return len(strings.Fields(string(out)))
}

func TestKillProcessGroup_BasicTermination(t *testing.T) {
	// Start a process that responds to SIGTERM
	cmd := exec.Command("sh", "-c", "sleep 60")
	SetProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	pid := cmd.Process.Pid

	testutil.Eventually(t, 2*time.Second, 5*time.Millisecond, func() bool {
		return pgidEstablished(pid)
	}, "process group for %d never established", pid)

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

	testutil.Eventually(t, 2*time.Second, 5*time.Millisecond, func() bool {
		return pgidEstablished(pid)
	}, "process group for %d never established", pid)

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

	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("failed to get pgid: %v", err)
	}
	// Wait for both `sleep 60` children to join the group — killing before
	// they exist would pass vacuously on an empty group.
	testutil.Eventually(t, 2*time.Second, 5*time.Millisecond, func() bool {
		return groupMembers(t, pgid) >= 3
	}, "children never joined process group %d", pgid)

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
	testutil.Eventually(t, 500*time.Millisecond, 10*time.Millisecond, func() bool {
		return syscall.Kill(-pgid, 0) == syscall.ESRCH
	}, "process group still running")
}

func TestKillProcessGroup_OrphanedGroupReaped(t *testing.T) {
	// Leader spawns a child then dies: the group lives on without its leader.
	// The old Getpgid(leaderPID) lookup returned ESRCH here and never signaled
	// the group; signaling -leaderPID must still reap it.
	cmd := exec.Command("sh", "-c", "sleep 60 & wait")
	SetProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	pid := cmd.Process.Pid

	pgid, err := syscall.Getpgid(pid)
	if err == nil {
		// Wait for the orphaned `sleep 60` child to join the group before
		// killing the leader, so the group genuinely outlives it.
		testutil.Eventually(t, 2*time.Second, 5*time.Millisecond, func() bool {
			return groupMembers(t, pgid) >= 2
		}, "child never joined process group %d", pgid)
	} else {
		t.Fatalf("failed to get pgid: %v", err)
	}

	// Kill only the leader; the orphaned sleep keeps the group alive.
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatalf("failed to kill leader: %v", err)
	}
	_ = cmd.Wait()

	if err := syscall.Kill(-pgid, 0); err == syscall.ESRCH {
		t.Skip("orphaned child did not survive leader exit in this environment")
	}

	err = KillProcessGroup(pid, KillOptions{GracePeriod: 50 * time.Millisecond})
	if err != nil {
		if err == syscall.EPERM {
			t.Skip("signal permissions restricted in this environment")
		}
		t.Errorf("KillProcessGroup returned error: %v", err)
	}

	testutil.Eventually(t, 500*time.Millisecond, 10*time.Millisecond, func() bool {
		return syscall.Kill(-pgid, 0) == syscall.ESRCH
	}, "orphaned process group still running")
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
