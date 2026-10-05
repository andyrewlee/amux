//go:build !windows

package process

import (
	"syscall"
	"testing"
)

func TestIsBenignStopErrorRecognizesTypedProcessGone(t *testing.T) {
	if !isBenignStopError(syscall.ESRCH) {
		t.Fatal("ESRCH should be benign")
	}
	if !isBenignStopError(syscall.ECHILD) {
		t.Fatal("ECHILD should be benign")
	}
	// EPERM on kill(-pgid) means the PID was recycled into a foreign group —
	// the tracked entry is stale, so stop-style callers treat it as gone.
	if !isBenignStopError(syscall.EPERM) {
		t.Fatal("EPERM should be benign for kill paths")
	}
}
