package tmux

import (
	"crypto/md5"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// This file covers the exported activity entry points in activity.go.
//
// The exec-free guards (empty session name / non-positive window) are asserted
// as pure unit tests because they short-circuit before any tmux command runs.
// ContentHash is fully pure and is exercised exhaustively.
//
// The subprocess-backed happy paths (sessionLatestActivitySeconds with a real
// session, SessionActiveWithin/SessionLatestActivity against live windows,
// ActiveAgentSessionsByActivity, SetMonitorActivityOn and SetStatusOff) run
// behind skipIfNoTmux against an isolated tmux server via testServer, mirroring
// the conventions in tmux_integration_test.go. They use real read-back
// assertions, never bare "did not crash" bodies.

// ---------------------------------------------------------------------------
// Pure guard tests (no subprocess)
// ---------------------------------------------------------------------------

func TestSessionLatestActivitySeconds_EmptyName(t *testing.T) {
	// Empty name returns (0, nil) before EnsureAvailable / any tmux command.
	got, err := sessionLatestActivitySeconds("", Options{})
	if err != nil {
		t.Fatalf("expected nil error for empty session name, got %v", err)
	}
	if got != 0 {
		t.Fatalf("expected 0 latest activity for empty name, got %d", got)
	}
}

func TestSessionActiveWithin_GuardsReturnInactive(t *testing.T) {
	tests := []struct {
		name    string
		session string
		window  time.Duration
	}{
		{name: "empty session", session: "", window: time.Minute},
		{name: "zero window", session: "sess", window: 0},
		{name: "negative window", session: "sess", window: -time.Second},
		{name: "empty session and zero window", session: "", window: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			active, err := SessionActiveWithin(tt.session, tt.window, Options{})
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if active {
				t.Fatalf("expected inactive for guarded input, got active")
			}
		})
	}
}

func TestSessionLatestActivity_EmptyNameReturnsNoActivity(t *testing.T) {
	// Empty name flows through sessionLatestActivitySeconds's guard and yields
	// the zero time with ok=false and no error.
	ts, ok, err := SessionLatestActivity("", Options{})
	if err != nil {
		t.Fatalf("expected nil error for empty session name, got %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for empty name, got ok=true")
	}
	if !ts.IsZero() {
		t.Fatalf("expected zero time for empty name, got %v", ts)
	}
}

// ---------------------------------------------------------------------------
// ContentHash (pure)
// ---------------------------------------------------------------------------

func TestContentHash_MatchesMD5(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "empty string", content: ""},
		{name: "ascii", content: "hello world"},
		{name: "newlines and tabs", content: "line1\n\tline2\r\n"},
		{name: "unicode", content: "héllo · 世界 · 🌍"},
		{name: "null bytes", content: "a\x00b\x00c"},
		{name: "long", content: strings.Repeat("amux-pane-content ", 4096)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContentHash(tt.content)
			want := md5.Sum([]byte(tt.content))
			if got != want {
				t.Fatalf("ContentHash(%q) = %x, want %x", tt.content, got, want)
			}
		})
	}
}

func TestContentHash_IsDeterministicAndSensitive(t *testing.T) {
	// Same input -> identical hash on repeated calls.
	a := ContentHash("stable input")
	b := ContentHash("stable input")
	if a != b {
		t.Fatalf("ContentHash is not deterministic: %x vs %x", a, b)
	}

	// A single-byte change must change the hash (the whole point of using it
	// for change detection).
	if ContentHash("stable input") == ContentHash("stable inpuT") {
		t.Fatalf("ContentHash collided on a single-byte change")
	}

	// Empty vs single space must differ.
	if ContentHash("") == ContentHash(" ") {
		t.Fatalf("ContentHash collided empty vs single space")
	}
}

func TestContentHash_NonZeroForKnownInput(t *testing.T) {
	// The fixed [16]byte width is guaranteed by the return type; assert instead
	// on a concrete, non-zero hash so the test exercises real behavior. md5 of
	// "anything" is a stable known value.
	got := ContentHash("anything")
	if got == ([16]byte{}) {
		t.Fatal("expected a non-zero hash for non-empty input")
	}
	want := md5.Sum([]byte("anything"))
	if got != want {
		t.Fatalf("ContentHash(\"anything\") = %x, want %x", got, want)
	}
}

// ---------------------------------------------------------------------------
// Subprocess-backed integration tests (isolated tmux server)
// ---------------------------------------------------------------------------

// globalOption reads back a global tmux option value for assertions.
func globalOption(t *testing.T, opts Options, key string) string {
	t.Helper()
	out, err := exec.Command("tmux", tmuxArgs(opts, "show-options", "-g", "-v", key)...).CombinedOutput()
	if err != nil {
		t.Fatalf("show-options -g %s: %v\n%s", key, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestSetMonitorActivityOn_EnablesGlobalOption(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	// Start from a known-off state so the assertion proves the toggle.
	out, err := exec.Command("tmux", tmuxArgs(opts, "set-option", "-g", "monitor-activity", "off")...).CombinedOutput()
	if err != nil {
		t.Fatalf("seed monitor-activity off: %v\n%s", err, out)
	}
	if got := globalOption(t, opts, "monitor-activity"); got != "off" {
		t.Fatalf("expected seeded monitor-activity=off, got %q", got)
	}

	if err := SetMonitorActivityOn(opts); err != nil {
		t.Fatalf("SetMonitorActivityOn: %v", err)
	}
	if got := globalOption(t, opts, "monitor-activity"); got != "on" {
		t.Fatalf("expected monitor-activity=on after SetMonitorActivityOn, got %q", got)
	}
}

func TestSetStatusOff_DisablesGlobalStatus(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	out, err := exec.Command("tmux", tmuxArgs(opts, "set-option", "-g", "status", "on")...).CombinedOutput()
	if err != nil {
		t.Fatalf("seed status on: %v\n%s", err, out)
	}
	if got := globalOption(t, opts, "status"); got != "on" {
		t.Fatalf("expected seeded status=on, got %q", got)
	}

	if err := SetStatusOff(opts); err != nil {
		t.Fatalf("SetStatusOff: %v", err)
	}
	if got := globalOption(t, opts, "status"); got != "off" {
		t.Fatalf("expected status=off after SetStatusOff, got %q", got)
	}
}

func TestSessionLatestActivitySeconds_LiveSession(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	before := time.Now().Unix()
	createSession(t, opts, "act-latest", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	latest, err := sessionLatestActivitySeconds("act-latest", opts)
	if err != nil {
		t.Fatalf("sessionLatestActivitySeconds: %v", err)
	}
	// tmux stamps window_activity at/after session creation.
	if latest < before {
		t.Fatalf("expected latest activity >= %d (creation), got %d", before, latest)
	}
	if latest > time.Now().Unix()+2 {
		t.Fatalf("latest activity %d is implausibly in the future", latest)
	}
}

func TestSessionLatestActivitySeconds_MissingSession(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	// A nonexistent session yields tmux exit code 1, which listTmux maps to an
	// empty result, so latest stays 0 with no error.
	latest, err := sessionLatestActivitySeconds("no-such-session", opts)
	if err != nil {
		t.Fatalf("expected nil error for missing session, got %v", err)
	}
	if latest != 0 {
		t.Fatalf("expected 0 latest activity for missing session, got %d", latest)
	}
}

func TestSessionActiveWithin_LiveSession(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "act-within", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	// A freshly created session is active within a generous window.
	active, err := SessionActiveWithin("act-within", time.Hour, opts)
	if err != nil {
		t.Fatalf("SessionActiveWithin (wide window): %v", err)
	}
	if !active {
		t.Fatal("expected freshly created session to be active within an hour")
	}

	// A nonexistent session is never active (latest == 0 short-circuit).
	active, err = SessionActiveWithin("no-such-session", time.Hour, opts)
	if err != nil {
		t.Fatalf("SessionActiveWithin (missing): %v", err)
	}
	if active {
		t.Fatal("expected missing session to be inactive")
	}
}

func TestSessionLatestActivity_LiveSession(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	created := time.Now()
	createSession(t, opts, "act-time", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	ts, ok, err := SessionLatestActivity("act-time", opts)
	if err != nil {
		t.Fatalf("SessionLatestActivity: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for a live session")
	}
	// Allow a 5s slop on either side of creation for whole-second tmux stamps.
	if ts.Before(created.Add(-5*time.Second)) || ts.After(created.Add(5*time.Second)) {
		t.Fatalf("activity time %v not near creation %v", ts, created)
	}

	// Missing session reports no activity.
	_, ok, err = SessionLatestActivity("no-such-session", opts)
	if err != nil {
		t.Fatalf("SessionLatestActivity (missing): %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for missing session")
	}
}

func TestActiveAgentSessionsByActivity_ReturnsTaggedSessions(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	// Tagged agent session: discovered via the @amux tag regardless of name.
	createSession(t, opts, "tagged-agent", "sleep 300")
	setTag(t, opts, "tagged-agent", "@amux", "1")
	setTag(t, opts, "tagged-agent", "@amux_workspace", "ws-1")
	setTag(t, opts, "tagged-agent", "@amux_tab", "tab-1")

	// Untagged amux-prefixed session: discovered via the name prefix fallback.
	createSession(t, opts, "amux-prefixed", "sleep 300")

	// Plain session with no tag and no amux- prefix: must be ignored.
	createSession(t, opts, "irrelevant", "sleep 300")
	time.Sleep(100 * time.Millisecond)

	sessions, err := ActiveAgentSessionsByActivity(time.Hour, opts)
	if err != nil {
		t.Fatalf("ActiveAgentSessionsByActivity: %v", err)
	}

	byName := make(map[string]SessionActivity, len(sessions))
	for _, s := range sessions {
		byName[s.Name] = s
	}

	tagged, ok := byName["tagged-agent"]
	if !ok {
		t.Fatalf("expected tagged-agent in results, got %+v", sessions)
	}
	if !tagged.Tagged {
		t.Errorf("expected tagged-agent.Tagged=true, got %+v", tagged)
	}
	if tagged.WorkspaceID != "ws-1" {
		t.Errorf("expected WorkspaceID=ws-1, got %q", tagged.WorkspaceID)
	}
	if tagged.TabID != "tab-1" {
		t.Errorf("expected TabID=tab-1, got %q", tagged.TabID)
	}

	prefixed, ok := byName["amux-prefixed"]
	if !ok {
		t.Fatalf("expected amux-prefixed in results, got %+v", sessions)
	}
	if prefixed.Tagged {
		t.Errorf("expected amux-prefixed.Tagged=false (name fallback), got %+v", prefixed)
	}

	if _, ok := byName["irrelevant"]; ok {
		t.Errorf("untagged non-amux session should be excluded, got %+v", sessions)
	}
}

func TestActiveAgentSessionsByActivity_WindowFiltersOldActivity(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "amux-recent", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	// A 1ns window enables the applyWindow branch (window>0); since tmux
	// window_activity is whole-second-truncated, now.Sub(activityTime) always
	// exceeds 1ns, so every session is excluded as older than the window.
	time.Sleep(5 * time.Millisecond)
	sessions, err := ActiveAgentSessionsByActivity(time.Nanosecond, opts)
	if err != nil {
		t.Fatalf("ActiveAgentSessionsByActivity (tiny window): %v", err)
	}
	for _, s := range sessions {
		if s.Name == "amux-recent" {
			t.Fatalf("expected amux-recent to be filtered out by a 1ns window, got %+v", sessions)
		}
	}
}
