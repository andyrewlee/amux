package data

import "testing"

func TestSessionStatusMachineFormallyChecked(t *testing.T) {
	if err := SessionStatusMachineCheck(); err != nil {
		t.Fatalf("session-status machine failed model checking: %v", err)
	}
}

func TestParseSessionStatus(t *testing.T) {
	cases := []struct {
		raw  string
		want SessionStatus
	}{
		{"", SessionRunning},
		{"  ", SessionRunning},
		{"Running", SessionRunning},
		{" detached ", SessionDetached},
		{"STOPPED", SessionStopped},
		{"zombie", SessionStatus("zombie")}, // unknown preserved for round-trip
	}
	for _, tc := range cases {
		if got := ParseSessionStatus(tc.raw); got != tc.want {
			t.Errorf("ParseSessionStatus(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestSessionStatusRunningLike(t *testing.T) {
	running := []SessionStatus{SessionRunning, SessionDetached, ""}
	for _, s := range running {
		if !s.RunningLike() {
			t.Errorf("%q should be running-like", s)
		}
	}
	stoppedLike := []SessionStatus{SessionStopped, "zombie"}
	for _, s := range stoppedLike {
		if s.RunningLike() {
			t.Errorf("%q should be stopped-like", s)
		}
	}
}

func TestNextSessionStatusRevivalAndDemotion(t *testing.T) {
	// A dead observation demotes every running-like status.
	for _, from := range []SessionStatus{SessionRunning, SessionDetached, SessionStopped, "zombie"} {
		if to, ok := NextSessionStatus(from, SessionObservedDead); !ok || to != SessionStopped {
			t.Fatalf("dead from %q: got (%q, %v), want stopped", from, to, ok)
		}
	}
	// A live observation revives stopped (and repairs unknown values) but
	// never converts detached to running.
	if to, _ := NextSessionStatus(SessionStopped, SessionObservedLive); to != SessionRunning {
		t.Fatalf("live from stopped: got %q, want running", to)
	}
	if to, _ := NextSessionStatus(SessionDetached, SessionObservedLive); to != SessionDetached {
		t.Fatalf("live from detached: got %q, want detached", to)
	}
	if to, _ := NextSessionStatus("zombie", SessionObservedLive); to != SessionRunning {
		t.Fatalf("live from unknown: got %q, want running (stopped-like repair)", to)
	}
}
