//go:build !windows

package process

import (
	"os/exec"
	"syscall"
	"time"
)

// KillOptions configures process group termination behavior.
type KillOptions struct {
	// GracePeriod is how long to wait for SIGTERM before sending SIGKILL.
	// Default: 200ms
	GracePeriod time.Duration
}

// KillProcessGroup sends SIGTERM to a process group, waits for the grace period,
// then sends SIGKILL if processes are still running.
// The leaderPID parameter is the process ID of the group leader.
func KillProcessGroup(leaderPID int, opts KillOptions) error {
	if opts.GracePeriod == 0 {
		opts.GracePeriod = 200 * time.Millisecond
	}

	// Get the actual process group ID
	pgid, err := syscall.Getpgid(leaderPID)
	if err != nil {
		// ESRCH means process already exited
		if err == syscall.ESRCH {
			return nil
		}
		return err
	}

	// Send SIGTERM to the entire process group (negative pgid)
	err = syscall.Kill(-pgid, syscall.SIGTERM)
	if err != nil {
		// ESRCH means process already exited
		if err == syscall.ESRCH {
			return nil
		}
		return err
	}

	// Wait for grace period, polling to see if process group exits
	deadline := time.Now().Add(opts.GracePeriod)
	for time.Now().Before(deadline) {
		// Check if any process in the group is still running
		err := syscall.Kill(-pgid, 0)
		if err == syscall.ESRCH {
			// Process group exited
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Process still running, send SIGKILL
	// EPERM can occur if the process group emptied during grace period
	err = syscall.Kill(-pgid, syscall.SIGKILL)
	if err != nil && err != syscall.ESRCH && err != syscall.EPERM {
		return err
	}

	return nil
}

// SetProcessGroup configures a command to run in its own process group.
func SetProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}
