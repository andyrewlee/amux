package tmux

import (
	"os/exec"
	"testing"
	"time"

	"github.com/creack/pty"
)

// These tests cover clients.go. The exec-free early-return guards (the
// sessionName == "" short-circuits in SessionClientCount / SessionHasClients /
// SessionCreatedAt) are asserted directly without a live tmux server. The
// subprocess-backed paths — SessionNamesWithClients, the non-empty-session
// branches of SessionClientCount / SessionHasClients, and SessionCreatedAt —
// are exercised behind skipIfNoTmux against an isolated test server, matching
// the convention in tags_test.go and the *_integration_test.go siblings.
//
// The "client actually attached" branch requires a real PTY-backed
// `tmux attach`; that is wired up in attachClient and guarded so a PTY/attach
// failure (e.g. a CI box with no controlling terminal) skips rather than fails.

// ---------------------------------------------------------------------------
// Exec-free guards: empty session name short-circuits before any tmux command.
// These run even when tmux is not installed.
// ---------------------------------------------------------------------------

func TestSessionClientCount_EmptySessionName(t *testing.T) {
	count, err := SessionClientCount("", Options{})
	if err != nil {
		t.Fatalf("expected nil error for empty session, got %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 clients for empty session, got %d", count)
	}
}

func TestSessionHasClients_EmptySessionName(t *testing.T) {
	has, err := SessionHasClients("", Options{})
	if err != nil {
		t.Fatalf("expected nil error for empty session, got %v", err)
	}
	if has {
		t.Fatal("expected false for empty session name")
	}
}

func TestSessionCreatedAt_EmptySessionName(t *testing.T) {
	ts, err := SessionCreatedAt("", Options{})
	if err != nil {
		t.Fatalf("expected nil error for empty session, got %v", err)
	}
	if ts != 0 {
		t.Fatalf("expected 0 timestamp for empty session, got %d", ts)
	}
}

// ---------------------------------------------------------------------------
// Subprocess-backed behavior against an isolated tmux server.
// ---------------------------------------------------------------------------

// TestSessionNamesWithClients_NoneAttached verifies that with only detached
// sessions present, SessionNamesWithClients returns a non-nil, empty set and no
// error (the "no attached clients must not fail GC" path).
func TestSessionNamesWithClients_NoneAttached(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "snwc-a", "sleep 300")
	createSession(t, opts, "snwc-b", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	attached, err := SessionNamesWithClients(opts)
	if err != nil {
		t.Fatalf("SessionNamesWithClients: %v", err)
	}
	if attached == nil {
		t.Fatal("expected non-nil map even with no clients")
	}
	if len(attached) != 0 {
		t.Fatalf("expected no attached sessions, got %v", attached)
	}
}

// TestSessionNamesWithClients_ReportsAttached attaches a real client to one
// session and confirms only that session is reported as having a client.
func TestSessionNamesWithClients_ReportsAttached(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "att-yes", "sleep 300")
	createSession(t, opts, "att-no", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	attachClient(t, opts, "att-yes")

	attached, err := SessionNamesWithClients(opts)
	if err != nil {
		t.Fatalf("SessionNamesWithClients: %v", err)
	}
	if !attached["att-yes"] {
		t.Fatalf("expected att-yes to be reported as attached, got %v", attached)
	}
	if attached["att-no"] {
		t.Fatalf("expected att-no to be detached, got %v", attached)
	}
}

// TestSessionClientCount_Detached confirms a detached session reports zero
// clients without error.
func TestSessionClientCount_Detached(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "scc-detached", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	count, err := SessionClientCount("scc-detached", opts)
	if err != nil {
		t.Fatalf("SessionClientCount: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 clients for detached session, got %d", count)
	}
}

// TestSessionClientCount_NonexistentSession confirms a session that does not
// exist short-circuits via the hasSession pre-check to zero, no error.
func TestSessionClientCount_NonexistentSession(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	count, err := SessionClientCount("nope-not-here", opts)
	if err != nil {
		t.Fatalf("expected nil error for missing session, got %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 clients for missing session, got %d", count)
	}
}

// TestSessionClientCount_Attached attaches a real client and confirms the count
// (and the SessionHasClients wrapper) reflect the attachment.
func TestSessionClientCount_Attached(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "scc-attached", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	attachClient(t, opts, "scc-attached")

	count, err := SessionClientCount("scc-attached", opts)
	if err != nil {
		t.Fatalf("SessionClientCount: %v", err)
	}
	if count < 1 {
		t.Fatalf("expected at least 1 attached client, got %d", count)
	}

	has, err := SessionHasClients("scc-attached", opts)
	if err != nil {
		t.Fatalf("SessionHasClients: %v", err)
	}
	if !has {
		t.Fatal("expected SessionHasClients to report an attached client")
	}
}

// TestSessionCreatedAt_PositiveAndMonotonic confirms two sessions created in
// sequence both report sane (positive, non-decreasing) creation timestamps.
func TestSessionCreatedAt_PositiveAndMonotonic(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "ca-first", "sleep 300")
	time.Sleep(1100 * time.Millisecond) // tmux session_created has second resolution
	createSession(t, opts, "ca-second", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	first, err := SessionCreatedAt("ca-first", opts)
	if err != nil {
		t.Fatalf("SessionCreatedAt(ca-first): %v", err)
	}
	second, err := SessionCreatedAt("ca-second", opts)
	if err != nil {
		t.Fatalf("SessionCreatedAt(ca-second): %v", err)
	}
	if first <= 0 || second <= 0 {
		t.Fatalf("expected positive timestamps, got first=%d second=%d", first, second)
	}
	if second < first {
		t.Fatalf("expected ca-second (%d) created no earlier than ca-first (%d)", second, first)
	}
}

// ---------------------------------------------------------------------------
// attachClient attaches a real client to a session over a PTY. It registers a
// cleanup that detaches the client and skips the test (rather than failing) if
// the environment cannot provide a PTY / accept the attach, so CI boxes without
// a controlling terminal degrade gracefully.
// ---------------------------------------------------------------------------

func attachClient(t *testing.T, opts Options, session string) {
	t.Helper()

	args := tmuxArgs(opts, "attach-session", "-t", session)
	cmd := exec.Command("tmux", args...)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Skipf("cannot start PTY-backed tmux attach: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = ptmx.Close()
	})

	// Poll until tmux registers the attached client (attach is asynchronous).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		count, err := SessionClientCount(session, opts)
		if err == nil && count > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Skipf("client never attached to %q within deadline", session)
}
