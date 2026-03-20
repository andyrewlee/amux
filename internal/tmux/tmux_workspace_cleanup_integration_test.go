package tmux

import (
	"testing"
	"time"
)

func TestKillWorkspaceSessionsKillsSandboxPrefixedSessions(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "amux-sandbox-ws123-agent", "sleep 300")
	createSession(t, opts, "amux-ws123-agent", "sleep 300")
	createSession(t, opts, "amux-other-agent", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	if err := KillWorkspaceSessions("ws123", opts); err != nil {
		t.Fatalf("KillWorkspaceSessions() error = %v", err)
	}

	sessions, err := ListSessions(opts)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	for _, session := range sessions {
		if session == "amux-sandbox-ws123-agent" || session == "amux-ws123-agent" {
			t.Fatalf("expected workspace session %q to be killed", session)
		}
	}
	foundOther := false
	for _, session := range sessions {
		if session == "amux-other-agent" {
			foundOther = true
		}
	}
	if !foundOther {
		t.Fatal("expected non-matching workspace session to remain")
	}
}

func TestKillWorkspaceSessionsKillsTaggedReboundSessionsWithOldNames(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "amux-oldws-agent", "sleep 300")
	setTag(t, opts, "amux-oldws-agent", "@amux", "1")
	setTag(t, opts, "amux-oldws-agent", "@amux_workspace", "ws123")
	time.Sleep(50 * time.Millisecond)

	if err := KillWorkspaceSessions("ws123", opts); err != nil {
		t.Fatalf("KillWorkspaceSessions() error = %v", err)
	}

	sessions, err := ListSessions(opts)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	for _, session := range sessions {
		if session == "amux-oldws-agent" {
			t.Fatalf("expected tagged rebound session %q to be killed", session)
		}
	}
}
