package app

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// Kill-family coverage for the tmuxOps wrapper in service_tmux.go. Split out of
// service_tmux_test.go to stay under the 500-line-per-file cap. These tests run
// against the isolated tmux server provided by gcTestServer (app_tmux_gc_test.go)
// and assert which sessions survive vs. get killed, plus the guarded no-op paths.

// ---------------------------------------------------------------------------
// KillSessionsMatchingTags
// ---------------------------------------------------------------------------

func TestTmuxOps_KillSessionsMatchingTags(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)
	ops := tmuxOps{}

	gcCreateSession(t, opts, "match-a", "sleep 300")
	gcCreateSession(t, opts, "match-b", "sleep 300")
	gcCreateSession(t, opts, "nomatch", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	gcSetTag(t, opts, "match-a", "@amux", "1")
	gcSetTag(t, opts, "match-a", "@amux_workspace", "doomed")
	gcSetTag(t, opts, "match-b", "@amux", "1")
	gcSetTag(t, opts, "match-b", "@amux_workspace", "doomed")
	gcSetTag(t, opts, "nomatch", "@amux", "1")
	gcSetTag(t, opts, "nomatch", "@amux_workspace", "safe")

	killed, err := ops.KillSessionsMatchingTags(map[string]string{"@amux_workspace": "doomed"}, opts)
	if err != nil {
		t.Fatalf("KillSessionsMatchingTags: %v", err)
	}
	if !killed {
		t.Fatal("KillSessionsMatchingTags = false, want true (sessions matched)")
	}
	if gcHasSession(t, opts, "match-a") || gcHasSession(t, opts, "match-b") {
		t.Fatal("matching sessions should have been killed")
	}
	if !gcHasSession(t, opts, "nomatch") {
		t.Fatal("non-matching session should survive")
	}

	// Second call with a tag matching nothing returns killed=false, no error.
	killed, err = ops.KillSessionsMatchingTags(map[string]string{"@amux_workspace": "nobody"}, opts)
	if err != nil {
		t.Fatalf("KillSessionsMatchingTags(no match): %v", err)
	}
	if killed {
		t.Fatal("KillSessionsMatchingTags(no match) = true, want false")
	}
}

func TestTmuxOps_KillSessionsMatchingTags_EmptyTags(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)
	// Empty tag set must short-circuit to killed=false without touching sessions.
	killed, err := tmuxOps{}.KillSessionsMatchingTags(map[string]string{}, opts)
	if err != nil {
		t.Fatalf("KillSessionsMatchingTags(empty): %v", err)
	}
	if killed {
		t.Fatal("KillSessionsMatchingTags(empty) = true, want false")
	}
}

// ---------------------------------------------------------------------------
// KillSessionsWithPrefix
// ---------------------------------------------------------------------------

func TestTmuxOps_KillSessionsWithPrefix(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)
	ops := tmuxOps{}

	gcCreateSession(t, opts, "pfx-one", "sleep 300")
	gcCreateSession(t, opts, "pfx-two", "sleep 300")
	gcCreateSession(t, opts, "other", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	if err := ops.KillSessionsWithPrefix("pfx-", opts); err != nil {
		t.Fatalf("KillSessionsWithPrefix: %v", err)
	}
	if gcHasSession(t, opts, "pfx-one") || gcHasSession(t, opts, "pfx-two") {
		t.Fatal("prefixed sessions should have been killed")
	}
	if !gcHasSession(t, opts, "other") {
		t.Fatal("non-prefixed session should survive")
	}
}

func TestTmuxOps_KillSessionsWithPrefix_EmptyPrefix(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)
	ops := tmuxOps{}

	gcCreateSession(t, opts, "keep-me", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	// Empty prefix must be a no-op (guard clause), leaving every session alive.
	if err := ops.KillSessionsWithPrefix("", opts); err != nil {
		t.Fatalf("KillSessionsWithPrefix(\"\"): %v", err)
	}
	if !gcHasSession(t, opts, "keep-me") {
		t.Fatal("empty prefix should not kill any session")
	}
}

// ---------------------------------------------------------------------------
// KillSessionsWithPrefixMissingTag
// ---------------------------------------------------------------------------

func TestTmuxOps_KillSessionsWithPrefixMissingTag(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)
	ops := tmuxOps{}

	// Legacy session (prefix match, tag absent) → killed.
	gcCreateSession(t, opts, "amux-legacy", "sleep 300")
	// Modern session (prefix match, tag present) → preserved.
	gcCreateSession(t, opts, "amux-owned", "sleep 300")
	// Wrong prefix → never considered.
	gcCreateSession(t, opts, "foreign", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	gcSetTag(t, opts, "amux-owned", "@amux_instance", "inst-42")

	if err := ops.KillSessionsWithPrefixMissingTag("amux-", "@amux_instance", opts); err != nil {
		t.Fatalf("KillSessionsWithPrefixMissingTag: %v", err)
	}
	if gcHasSession(t, opts, "amux-legacy") {
		t.Fatal("legacy session lacking the tag should have been killed")
	}
	if !gcHasSession(t, opts, "amux-owned") {
		t.Fatal("session with the instance tag should be preserved")
	}
	if !gcHasSession(t, opts, "foreign") {
		t.Fatal("non-prefixed session should be untouched")
	}
}

func TestTmuxOps_KillSessionsWithPrefixMissingTag_EmptyArgs(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)
	ops := tmuxOps{}

	gcCreateSession(t, opts, "amux-survivor", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	// Either empty prefix or empty tag short-circuits to a no-op.
	if err := ops.KillSessionsWithPrefixMissingTag("", "@amux_instance", opts); err != nil {
		t.Fatalf("empty prefix: %v", err)
	}
	if err := ops.KillSessionsWithPrefixMissingTag("amux-", "", opts); err != nil {
		t.Fatalf("empty tag: %v", err)
	}
	if !gcHasSession(t, opts, "amux-survivor") {
		t.Fatal("no-op call should not kill any session")
	}
}

// ---------------------------------------------------------------------------
// KillWorkspaceSessions
// ---------------------------------------------------------------------------

func TestTmuxOps_KillWorkspaceSessions(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)
	ops := tmuxOps{}

	// KillWorkspaceSessions derives the prefix SessionName("amux", wsID)+"-".
	wsID := "myws"
	prefix := tmux.SessionName("amux", wsID) + "-"
	target := prefix + "tab1"
	other := tmux.SessionName("amux", "otherws") + "-tab1"

	gcCreateSession(t, opts, target, "sleep 300")
	gcCreateSession(t, opts, other, "sleep 300")
	time.Sleep(50 * time.Millisecond)

	if err := ops.KillWorkspaceSessions(wsID, opts); err != nil {
		t.Fatalf("KillWorkspaceSessions: %v", err)
	}
	if gcHasSession(t, opts, target) {
		t.Fatalf("workspace session %q should have been killed", target)
	}
	if !gcHasSession(t, opts, other) {
		t.Fatalf("session for a different workspace %q should survive", other)
	}
}

func TestTmuxOps_KillWorkspaceSessions_EmptyID(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)
	ops := tmuxOps{}

	gcCreateSession(t, opts, "amux-keep-tab1", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	// Empty workspace ID is a guarded no-op.
	if err := ops.KillWorkspaceSessions("", opts); err != nil {
		t.Fatalf("KillWorkspaceSessions(\"\"): %v", err)
	}
	if !gcHasSession(t, opts, "amux-keep-tab1") {
		t.Fatal("empty workspace ID should not kill any session")
	}
}
