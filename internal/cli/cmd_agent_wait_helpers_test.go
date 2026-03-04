package cli

import (
	"testing"
	"time"
)

func TestEffectiveInitialChangeTimeout_UsesRequestedWaitTimeout(t *testing.T) {
	origInitialTimeout := waitResponseInitialChangeTimeout
	waitResponseInitialChangeTimeout = 90 * time.Second
	defer func() { waitResponseInitialChangeTimeout = origInitialTimeout }()

	got := effectiveInitialChangeTimeout(12 * time.Minute)
	if got != 12*time.Minute {
		t.Fatalf("effectiveInitialChangeTimeout(12m) = %v, want 12m", got)
	}

	got = effectiveInitialChangeTimeout(0)
	if got != 90*time.Second {
		t.Fatalf("effectiveInitialChangeTimeout(0) = %v, want 90s fallback", got)
	}
}
