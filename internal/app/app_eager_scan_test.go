package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
)

// TestUpdate_TabReattachedSchedulesEagerScan proves a reattached agent triggers
// an immediate activity rescan instead of waiting up to one ticker interval, and
// that a second reattach while a scan is in flight only marks a rescan pending.
func TestUpdate_TabReattachedSchedulesEagerScan(t *testing.T) {
	app := &App{tmuxAvailable: true}

	app.update(messages.TabReattached{WorkspaceID: "ws-a"})
	if !app.tmuxActivity.scanInFlight {
		t.Fatal("expected an eager scan to be scheduled on TabReattached")
	}
	if app.tmuxActivity.token != 1 {
		t.Fatalf("expected scan token incremented to 1, got %d", app.tmuxActivity.token)
	}

	// A second reattach while the first scan is in flight must coalesce: no new
	// token, only a pending rescan.
	app.update(messages.TabReattached{WorkspaceID: "ws-b"})
	if app.tmuxActivity.token != 1 {
		t.Fatalf("expected in-flight reattach to coalesce (token stays 1), got %d", app.tmuxActivity.token)
	}
	if !app.tmuxActivity.rescanPending {
		t.Fatal("expected in-flight second reattach to set rescan-pending")
	}
}

// TestUpdate_TabCreatedSchedulesEagerScan proves a freshly created agent tab
// schedules an immediate activity rescan.
func TestUpdate_TabCreatedSchedulesEagerScan(t *testing.T) {
	app := &App{
		tmuxAvailable: true,
		center:        center.New(nil),
	}

	app.update(messages.TabCreated{Name: "claude"})
	if !app.tmuxActivity.scanInFlight {
		t.Fatal("expected an eager scan to be scheduled on TabCreated")
	}
	if app.tmuxActivity.token != 1 {
		t.Fatalf("expected scan token incremented to 1, got %d", app.tmuxActivity.token)
	}
}

// TestUpdate_TabReattachedNoScanWhenTmuxUnavailable proves the eager scan is
// suppressed when tmux is unavailable, avoiding no-op churn.
func TestUpdate_TabReattachedNoScanWhenTmuxUnavailable(t *testing.T) {
	app := &App{tmuxAvailable: false}

	app.update(messages.TabReattached{WorkspaceID: "ws-a"})
	if app.tmuxActivity.scanInFlight {
		t.Fatal("expected no scan scheduled when tmux is unavailable")
	}
	if app.tmuxActivity.token != 0 {
		t.Fatalf("expected scan token untouched, got %d", app.tmuxActivity.token)
	}
}
