package cli

import (
	"testing"
	"time"
)

func TestNewServicesUsesTimeoutOverride(t *testing.T) {
	prevTimeout := setCLITmuxTimeoutOverride(0)
	defer setCLITmuxTimeoutOverride(prevTimeout)

	const want = 1750 * time.Millisecond
	setCLITmuxTimeoutOverride(want)

	svc, err := NewServices("test-v1")
	if err != nil {
		t.Fatalf("NewServices() error = %v", err)
	}
	if svc.TmuxOpts.CommandTimeout != want {
		t.Fatalf("tmux timeout = %v, want %v", svc.TmuxOpts.CommandTimeout, want)
	}
}
