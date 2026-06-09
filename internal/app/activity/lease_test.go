package activity

import (
	"testing"
	"time"
)

// TestSnapshotFresh covers the forward-skew cap that keeps a future-dated
// snapshot from pinning followers to a stale active set forever.
func TestSnapshotFresh(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"far future rejected", now.Add(time.Minute), false},
		{"small forward skew accepted", now.Add(time.Second), true},
		{"recent past accepted", now.Add(-5 * time.Second), true},
		{"stale past rejected", now.Add(-15 * time.Second), false},
		{"exactly now accepted", now, true},
		{"future at skew boundary accepted", now.Add(SnapshotFutureSkewTolerance), true},
		{"future just past boundary rejected", now.Add(SnapshotFutureSkewTolerance + time.Millisecond), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := snapshotFresh(tc.at, now); got != tc.want {
				t.Fatalf("snapshotFresh(%v, %v) = %v, want %v", tc.at, now, got, tc.want)
			}
		})
	}
}

// TestSnapshotFresh_RoundTripThroughEncodeDecode drives a far-future timestamp
// through EncodeSnapshot/DecodeSnapshot to confirm the decoded timestamp still
// trips the skew cap (no loss of precision flips the freshness decision).
func TestSnapshotFresh_RoundTripThroughEncodeDecode(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	future := now.Add(time.Minute)

	encoded := EncodeSnapshot(map[string]bool{"ws-a": true}, 3, future)
	parsed, epoch, at, ok := DecodeSnapshot(encoded)
	if !ok {
		t.Fatal("expected snapshot to decode")
	}
	if epoch != 3 {
		t.Fatalf("expected epoch 3, got %d", epoch)
	}
	if !parsed["ws-a"] {
		t.Fatalf("expected ws-a in decoded set, got %v", parsed)
	}
	if snapshotFresh(at, now) {
		t.Fatalf("far-future decoded timestamp %v must be rejected as stale", at)
	}
}
