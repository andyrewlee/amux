//go:build !windows

package process

import (
	"syscall"
	"testing"
)

func TestIsBenignStopErrorRecognizesTypedProcessGone(t *testing.T) {
	if !isBenignStopError(syscall.ESRCH) {
		t.Fatalf("expected ESRCH to be treated as benign stop error")
	}
	if !isBenignStopError(syscall.ECHILD) {
		t.Fatalf("expected ECHILD to be treated as benign stop error")
	}
}
