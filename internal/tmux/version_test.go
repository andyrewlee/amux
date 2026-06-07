package tmux

import (
	"os/exec"
	"testing"
	"time"
)

func TestParseTmuxVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in            string
		wantMajor     int
		wantMinor     int
		wantSuffix    string
		wantParseFail bool
	}{
		{in: "tmux 3.6a", wantMajor: 3, wantMinor: 6, wantSuffix: "a"},
		{in: "tmux 3.2a", wantMajor: 3, wantMinor: 2, wantSuffix: "a"},
		{in: "tmux 3.4", wantMajor: 3, wantMinor: 4, wantSuffix: ""},
		{in: "tmux next-3.5", wantMajor: 3, wantMinor: 5, wantSuffix: ""},
		{in: "3.10", wantMajor: 3, wantMinor: 10, wantSuffix: ""},
		{in: "tmux unknownbuild", wantParseFail: true},
		{in: "", wantParseFail: true},
	}
	for _, c := range cases {
		got, err := ParseTmuxVersion(c.in)
		if c.wantParseFail {
			if err == nil {
				t.Errorf("ParseTmuxVersion(%q): expected error, got %+v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseTmuxVersion(%q): unexpected error %v", c.in, err)
			continue
		}
		if got.Major != c.wantMajor || got.Minor != c.wantMinor || got.Suffix != c.wantSuffix {
			t.Errorf("ParseTmuxVersion(%q) = {%d %d %q}, want {%d %d %q}",
				c.in, got.Major, got.Minor, got.Suffix, c.wantMajor, c.wantMinor, c.wantSuffix)
		}
	}
}

func TestTmuxVersionAtLeast(t *testing.T) {
	t.Parallel()
	v36a := TmuxVersion{Major: 3, Minor: 6, Suffix: "a"}
	v36 := TmuxVersion{Major: 3, Minor: 6}
	cases := []struct {
		v            TmuxVersion
		major, minor int
		suffix       string
		want         bool
	}{
		{v36a, 3, 2, "a", true},                            // 3.6a >= 3.2a
		{v36a, 3, 6, "", true},                             // 3.6a >= 3.6
		{v36a, 3, 6, "a", true},                            // 3.6a >= 3.6a
		{v36a, 3, 6, "b", false},                           // 3.6a < 3.6b
		{v36, 3, 6, "a", false},                            // 3.6 < 3.6a
		{v36a, 4, 0, "", false},                            // 3.6a < 4.0
		{TmuxVersion{Major: 3, Minor: 10}, 3, 9, "", true}, // numeric minor: 3.10 >= 3.9
	}
	for _, c := range cases {
		if got := c.v.AtLeast(c.major, c.minor, c.suffix); got != c.want {
			t.Errorf("%+v.AtLeast(%d,%d,%q) = %v, want %v", c.v, c.major, c.minor, c.suffix, got, c.want)
		}
	}
}

// TestServerVersionMatchesRunningTmux confirms detection works against the
// installed tmux.
func TestServerVersionMatchesRunningTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	v, err := ServerVersion()
	if err != nil {
		t.Fatalf("ServerVersion: %v", err)
	}
	if v.Major < 1 || v.Raw == "" {
		t.Fatalf("implausible tmux version detected: %+v", v)
	}
}

// TestCapturePaneSnapshotModeStateParsesOnRunningTmux guards the hardcoded
// pane-mode capture format against tmux version drift. The format string lists
// fixed fields; if a future tmux renames/reorders them, parsePaneModeState
// silently degrades to HasState:false with no signal. Capturing a live pane and
// asserting the mode state parses turns that silent degradation into a loud
// failure that names the running version.
func TestCapturePaneSnapshotModeStateParsesOnRunningTmux(t *testing.T) {
	opts := realTmuxServerWithKeepalive(t)
	createSession(t, opts, "snap", "sleep 300")

	var snap PaneSnapshot
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s, err := CapturePaneSnapshot("snap", opts)
		if err == nil && s.ModeState.HasState {
			snap = s
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !snap.ModeState.HasState {
		v, _ := ServerVersion()
		t.Fatalf("pane-mode capture format did not parse on tmux %q; the hardcoded format may have drifted with the tmux version", v.Raw)
	}
}
