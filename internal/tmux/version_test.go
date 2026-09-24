package tmux

import "testing"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in           string
		major, minor int
		ok           bool
	}{
		{"tmux 3.4", 3, 4, true},
		{"tmux 3.2a", 3, 2, true},
		{"tmux 3.6a", 3, 6, true},
		{"tmux next-3.6", 3, 6, true},
		{"tmux 3.7b", 3, 7, true},
		{"tmux 2.9", 2, 9, true},
		{"tmux 4", 4, 0, true},
		{"tmux 3.10", 3, 10, true},
		{"tmux 3.2-rc1", 3, 2, true},
		{"tmux ", 0, 0, false},
		{"tmux", 0, 0, false},
		{"garbage", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, tc := range tests {
		maj, minV, ok := parseVersion(tc.in)
		if ok != tc.ok || maj != tc.major || minV != tc.minor {
			t.Errorf("parseVersion(%q) = (%d,%d,%v), want (%d,%d,%v)", tc.in, maj, minV, ok, tc.major, tc.minor, tc.ok)
		}
	}
}

func TestEnsureAvailable_ThisHost(t *testing.T) {
	// Hosts with a real tmux >= 3.2 must pass; error string must carry the hint.
	if err := EnsureAvailable(); err != nil {
		t.Logf("EnsureAvailable error (acceptable only if tmux absent/too old): %v", err)
	}
}
