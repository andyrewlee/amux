//go:build windows

package process

import (
	"log/slog"
	"os"
	"os/exec"
	"time"
)

// KillOptions configures process termination behavior.
type KillOptions struct {
	// GracePeriod is how long to wait before forcing termination.
	// Default: 200ms
	GracePeriod time.Duration
}

// KillProcessGroup attempts to terminate only the leader process on Windows.
// Note: Windows lacks Unix-style process groups; child processes may remain.
func KillProcessGroup(leaderPID int, opts KillOptions) error {
	if leaderPID <= 0 {
		return nil
	}
	if opts.GracePeriod == 0 {
		opts.GracePeriod = 200 * time.Millisecond
	}

	proc, err := os.FindProcess(leaderPID)
	if err != nil {
		return err
	}

	if err := proc.Signal(os.Interrupt); err != nil {
		slog.Debug("best-effort interrupt signal failed", "pid", leaderPID, "error", err)
	}
	if opts.GracePeriod > 0 {
		time.Sleep(opts.GracePeriod)
	}

	return proc.Kill()
}

// SetProcessGroup is a no-op on Windows.
func SetProcessGroup(cmd *exec.Cmd) {
	_ = cmd
}
