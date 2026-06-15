package tmux

import (
	"sort"
	"testing"
	"time"
)

// These tests cover the previously-untested functions in tmux_sessions.go:
// ListSessions, KillSessionsWithPrefix, and KillSessionsWithPrefixMissingTag.
// The exec-free early-return guards (KillSessionsWithPrefix's empty-prefix
// short-circuit and KillSessionsWithPrefixMissingTag's empty-prefix/empty-tag
// short-circuits, all of which return before EnsureAvailable / any tmux
// command) are asserted directly without a live tmux server. The
// subprocess-backed behavior — ListSessions and the prefix-matching kill paths
// — is exercised behind skipIfNoTmux against an isolated test server, matching
// the convention in clients_test.go / tags_test.go and the *_integration_test.go
// siblings (testServer / createSession / setTag helpers).
//
// AmuxSessionsByWorkspace already retains its existing live coverage in
// tmux_integration_test.go; this file intentionally does not duplicate that
// grouping coverage here.

// ---------------------------------------------------------------------------
// Exec-free guards: these never reach tmux, so they run even without tmux.
// ---------------------------------------------------------------------------

// TestKillSessionsWithPrefix_EmptyPrefixNoOp covers the guard that returns nil
// before EnsureAvailable / any tmux command when the prefix is empty. A blank
// prefix would otherwise match (and kill) every session, so this guard matters.
func TestKillSessionsWithPrefix_EmptyPrefixNoOp(t *testing.T) {
	if err := KillSessionsWithPrefix("", Options{}); err != nil {
		t.Fatalf("expected nil error for empty prefix, got %v", err)
	}
}

// TestKillSessionsWithPrefixMissingTag_EmptyInputGuards covers the guards that
// no-op (return nil) before any tmux command when either the prefix or the tag
// is blank.
func TestKillSessionsWithPrefixMissingTag_EmptyInputGuards(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		tag    string
	}{
		{name: "empty prefix", prefix: "", tag: "@amux_instance"},
		{name: "empty tag", prefix: "amux-", tag: ""},
		{name: "both empty", prefix: "", tag: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := KillSessionsWithPrefixMissingTag(tc.prefix, tc.tag, Options{}); err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ListSessions — subprocess-backed against an isolated tmux server.
// ---------------------------------------------------------------------------

// TestListSessions_ReportsCreatedSessions confirms ListSessions returns exactly
// the names of the sessions created on the isolated server.
func TestListSessions_ReportsCreatedSessions(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "ls-alpha", "sleep 300")
	createSession(t, opts, "ls-beta", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	got, err := ListSessions(opts)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	sort.Strings(got)
	want := []string{"ls-alpha", "ls-beta"}
	if !equalStrings(got, want) {
		t.Fatalf("expected sessions %v, got %v", want, got)
	}
}

// TestListSessions_EmptyServer confirms ListSessions returns an empty result
// (not an error) when the server has no sessions. tmux exits non-zero with
// "no server running"/"no sessions" here, which listTmux must treat as empty.
func TestListSessions_EmptyServer(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	got, err := ListSessions(opts)
	if err != nil {
		t.Fatalf("expected nil error on empty server, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no sessions, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// KillSessionsWithPrefix — subprocess-backed.
// ---------------------------------------------------------------------------

// TestKillSessionsWithPrefix_KillsOnlyMatching verifies that only sessions
// whose name starts with the prefix are killed; non-matching ones survive.
func TestKillSessionsWithPrefix_KillsOnlyMatching(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "amux-keep-1", "sleep 300")
	createSession(t, opts, "amux-keep-2", "sleep 300")
	createSession(t, opts, "other-session", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	if err := KillSessionsWithPrefix("amux-keep-", opts); err != nil {
		t.Fatalf("KillSessionsWithPrefix: %v", err)
	}

	got, err := ListSessions(opts)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if !equalStrings(got, []string{"other-session"}) {
		t.Fatalf("expected only other-session to survive, got %v", got)
	}
}

// TestKillSessionsWithPrefix_NoMatchLeavesAll confirms that a prefix matching
// no live session is a no-op (returns nil, kills nothing).
func TestKillSessionsWithPrefix_NoMatchLeavesAll(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "alpha", "sleep 300")
	createSession(t, opts, "beta", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	if err := KillSessionsWithPrefix("no-such-prefix-", opts); err != nil {
		t.Fatalf("KillSessionsWithPrefix: %v", err)
	}

	got, err := ListSessions(opts)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	sort.Strings(got)
	if !equalStrings(got, []string{"alpha", "beta"}) {
		t.Fatalf("expected both sessions to survive, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// KillSessionsWithPrefixMissingTag — subprocess-backed.
// ---------------------------------------------------------------------------

// TestKillSessionsWithPrefixMissingTag_KillsOnlyUntagged exercises the legacy
// GC behavior: among prefix-matching sessions, only those whose tag is empty
// are killed; a prefix session carrying the tag is preserved, and a session
// outside the prefix is untouched even though it also lacks the tag.
func TestKillSessionsWithPrefixMissingTag_KillsOnlyUntagged(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	const tag = "@amux_instance"

	// Prefix-matching, no tag → should be killed.
	createSession(t, opts, "amux-legacy-1", "sleep 300")
	// Prefix-matching, tag set → must survive (owned by another instance).
	createSession(t, opts, "amux-modern-1", "sleep 300")
	// Outside the prefix, no tag → must survive (prefix gate).
	createSession(t, opts, "unrelated-1", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	setTag(t, opts, "amux-modern-1", tag, "instance-xyz")

	if err := KillSessionsWithPrefixMissingTag("amux-", tag, opts); err != nil {
		t.Fatalf("KillSessionsWithPrefixMissingTag: %v", err)
	}

	got, err := ListSessions(opts)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	sort.Strings(got)
	want := []string{"amux-modern-1", "unrelated-1"}
	if !equalStrings(got, want) {
		t.Fatalf("expected %v to survive, got %v", want, got)
	}
}

// TestKillSessionsWithPrefixMissingTag_WhitespaceTagTreatedAsEmpty confirms a
// tag value that is only whitespace counts as "missing" (the strings.TrimSpace
// check), so the session is killed like a truly untagged one.
func TestKillSessionsWithPrefixMissingTag_WhitespaceTagTreatedAsEmpty(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	const tag = "@amux_instance"

	createSession(t, opts, "amux-ws-1", "sleep 300")
	time.Sleep(50 * time.Millisecond)
	setTag(t, opts, "amux-ws-1", tag, "   ")

	if err := KillSessionsWithPrefixMissingTag("amux-", tag, opts); err != nil {
		t.Fatalf("KillSessionsWithPrefixMissingTag: %v", err)
	}

	got, err := ListSessions(opts)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected whitespace-tagged session to be killed, got %v", got)
	}
}
