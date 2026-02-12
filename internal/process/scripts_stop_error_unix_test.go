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
}
