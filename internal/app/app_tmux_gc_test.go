package app

import (
	"testing"
	"time"
)

func TestSelectStaleTerminalSessions(t *testing.T) {
	now := time.Unix(1000, 0)
	ttl := 10 * time.Second

	sessions := []terminalSessionInfo{
		{Name: "keep-clients", CreatedAt: 980, HasClients: true},
		{Name: "keep-recent", CreatedAt: 995, HasClients: false},
		{Name: "keep-unknown", CreatedAt: 0, HasClients: false},
		{Name: "kill-stale", CreatedAt: 990, HasClients: false},
	}

	out := selectStaleTerminalSessions(sessions, now, ttl)
	if len(out) != 1 || out[0] != "kill-stale" {
		t.Fatalf("expected only kill-stale, got %v", out)
	}
}

func TestShouldSkipTerminalForInstance(t *testing.T) {
	active := map[string]bool{
		"other": true,
	}
	if shouldSkipTerminalForInstance("other", "current", active) != true {
		t.Fatal("expected skip for active other instance")
	}
	if shouldSkipTerminalForInstance("current", "current", active) != false {
		t.Fatal("expected no skip for current instance")
	}
	if shouldSkipTerminalForInstance("", "current", active) != false {
		t.Fatal("expected no skip for empty instance")
	}
	if shouldSkipTerminalForInstance("stale", "current", active) != false {
		t.Fatal("expected no skip for inactive instance")
	}
}
