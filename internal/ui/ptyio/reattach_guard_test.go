package ptyio

import (
	"testing"
	"time"
)

func TestReattachGuardBeginStampsAcquisition(t *testing.T) {
	var g ReattachGuard
	g.Begin()
	if !g.InFlight {
		t.Fatal("Begin did not set InFlight")
	}
	if g.StartedAt.IsZero() {
		t.Fatal("Begin did not stamp StartedAt")
	}
}

func TestReattachGuardSweep(t *testing.T) {
	now := time.Now()
	old := now.Add(-2 * ReattachStallTimeout)
	tests := []struct {
		name         string
		guard        ReattachGuard
		running      bool
		wantRelease  bool
		wantInFlight bool
		wantStamp    time.Time
	}{
		{
			name:        "not in flight is skipped",
			guard:       ReattachGuard{},
			wantRelease: false,
			wantStamp:   time.Time{},
		},
		{
			name:         "running item is skipped even if flag lingers",
			guard:        ReattachGuard{InFlight: true, StartedAt: old},
			running:      true,
			wantRelease:  false,
			wantInFlight: true,
			wantStamp:    old,
		},
		{
			name:         "zero stamp arms the clock instead of releasing",
			guard:        ReattachGuard{InFlight: true},
			wantRelease:  false,
			wantInFlight: true,
			wantStamp:    now,
		},
		{
			name:         "fresh stamp is not released",
			guard:        ReattachGuard{InFlight: true, StartedAt: now},
			wantRelease:  false,
			wantInFlight: true,
			wantStamp:    now,
		},
		{
			name:        "expired lock is released",
			guard:       ReattachGuard{InFlight: true, StartedAt: old},
			wantRelease: true,
			wantStamp:   old,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := tt.guard
			if released := g.Sweep(now, tt.running); released != tt.wantRelease {
				t.Fatalf("Sweep released = %v, want %v", released, tt.wantRelease)
			}
			if g.InFlight != tt.wantInFlight {
				t.Fatalf("InFlight = %v, want %v", g.InFlight, tt.wantInFlight)
			}
			if !g.StartedAt.Equal(tt.wantStamp) {
				t.Fatalf("StartedAt = %v, want %v", g.StartedAt, tt.wantStamp)
			}
		})
	}
}
