package tmux

import "testing"

// liveSessions returns the set of session names currently on the server.
func liveSessions(t *testing.T, opts Options) map[string]bool {
	t.Helper()
	names, err := ListSessions(opts)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}

// TestKillWorkspaceSessions_ScopesToWorkspaceAndAvoidsPrefixCollision is the
// core delete-safety contract: killing wsA must take ONLY wsA's sessions, never
// a sibling's — and the prefix "amux-wsa-" must not collide with "amux-wsa2-0".
func TestKillWorkspaceSessions_ScopesToWorkspaceAndAvoidsPrefixCollision(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	wsA0 := SessionName("amux", "wsa") + "-0"
	wsA1 := SessionName("amux", "wsa") + "-1"
	wsB0 := SessionName("amux", "wsb") + "-0"
	wsA2 := SessionName("amux", "wsa2") + "-0"
	for _, name := range []string{wsA0, wsA1, wsB0, wsA2} {
		createSession(t, opts, name, "sleep 60")
	}

	if err := KillWorkspaceSessions("wsa", opts); err != nil {
		t.Fatalf("KillWorkspaceSessions: %v", err)
	}

	live := liveSessions(t, opts)
	if live[wsA0] || live[wsA1] {
		t.Fatalf("expected wsA sessions killed, live=%v", live)
	}
	if !live[wsB0] {
		t.Fatalf("sibling wsB session must survive, live=%v", live)
	}
	if !live[wsA2] {
		t.Fatalf("prefix-collision wsA2 session must survive (amux-wsa- must not match amux-wsa2-0), live=%v", live)
	}
}

// TestKillSessionsMatchingTags_ScopesByTag proves tag-based teardown kills only
// the matching workspace's sessions and leaves siblings alone, and that a
// no-match query kills nothing.
func TestKillSessionsMatchingTags_ScopesByTag(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	wsA0 := SessionName("amux", "wsa") + "-0"
	wsA1 := SessionName("amux", "wsa") + "-1"
	wsB0 := SessionName("amux", "wsb") + "-0"
	wsA2 := SessionName("amux", "wsa2") + "-0"
	tagged := map[string]string{wsA0: "wsa", wsA1: "wsa", wsB0: "wsb", wsA2: "wsa2"}
	for name, wsID := range tagged {
		createSession(t, opts, name, "sleep 60")
		setTag(t, opts, name, "@amux", "1")
		setTag(t, opts, name, "@amux_workspace", wsID)
	}

	killed, err := KillSessionsMatchingTags(map[string]string{"@amux": "1", "@amux_workspace": "wsa"}, opts)
	if err != nil {
		t.Fatalf("KillSessionsMatchingTags: %v", err)
	}
	if !killed {
		t.Fatal("expected killed=true when matching sessions exist")
	}
	live := liveSessions(t, opts)
	if live[wsA0] || live[wsA1] {
		t.Fatalf("expected wsA sessions killed by tag match, live=%v", live)
	}
	if !live[wsB0] || !live[wsA2] {
		t.Fatalf("non-matching siblings must survive, live=%v", live)
	}

	// No-match query kills nothing and reports killed=false.
	beforeNoMatch := liveSessions(t, opts)
	killed, err = KillSessionsMatchingTags(map[string]string{"@amux": "1", "@amux_workspace": "nope"}, opts)
	if err != nil {
		t.Fatalf("KillSessionsMatchingTags(no-match): %v", err)
	}
	if killed {
		t.Fatal("expected killed=false for a no-match tag query")
	}
	afterNoMatch := liveSessions(t, opts)
	if len(beforeNoMatch) != len(afterNoMatch) {
		t.Fatalf("no-match query must not kill anything: before=%v after=%v", beforeNoMatch, afterNoMatch)
	}
}

// TestKillPrimitives_EmptyInputGuards proves the empty-input guards short-circuit
// before touching tmux (no server required).
func TestKillPrimitives_EmptyInputGuards(t *testing.T) {
	opts := Options{ServerName: "amux-nonexistent-guard", ConfigPath: "/dev/null"}
	if err := KillWorkspaceSessions("", opts); err != nil {
		t.Fatalf("KillWorkspaceSessions(\"\") should be a nil no-op, got %v", err)
	}
	killed, err := KillSessionsMatchingTags(nil, opts)
	if err != nil || killed {
		t.Fatalf("KillSessionsMatchingTags(nil) should be (false, nil), got (%v, %v)", killed, err)
	}
}
