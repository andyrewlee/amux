package app

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

func TestMarkInput(t *testing.T) {
	t.Run("sets pending latency and advances timestamp", func(t *testing.T) {
		app := &App{}
		if app.pendingInputLatency {
			t.Fatal("expected pendingInputLatency to be false before markInput")
		}
		if !app.lastInputAt.IsZero() {
			t.Fatal("expected lastInputAt to be zero before markInput")
		}

		before := time.Now()
		app.markInput()
		after := time.Now()

		if !app.pendingInputLatency {
			t.Fatal("expected pendingInputLatency to be true after markInput")
		}
		if app.lastInputAt.Before(before) || app.lastInputAt.After(after) {
			t.Fatalf("expected lastInputAt within [%v, %v], got %v", before, after, app.lastInputAt)
		}
	})

	t.Run("overwrites a previously cleared pending flag", func(t *testing.T) {
		// Simulate the view layer having consumed a prior input by clearing
		// the pending flag, then assert a fresh input re-arms it.
		app := &App{
			lastInputAt:         time.Now().Add(-time.Hour),
			pendingInputLatency: false,
		}
		prior := app.lastInputAt

		app.markInput()

		if !app.pendingInputLatency {
			t.Fatal("expected pendingInputLatency to be re-armed by markInput")
		}
		if !app.lastInputAt.After(prior) {
			t.Fatalf("expected lastInputAt to advance past %v, got %v", prior, app.lastInputAt)
		}
	})

	t.Run("repeated calls keep pending true and monotonically advance", func(t *testing.T) {
		app := &App{}
		app.markInput()
		first := app.lastInputAt

		// Sleep a touch so the wall clock advances even on coarse-grained timers.
		time.Sleep(time.Millisecond)
		app.markInput()
		second := app.lastInputAt

		if !app.pendingInputLatency {
			t.Fatal("expected pendingInputLatency to remain true across calls")
		}
		if second.Before(first) {
			t.Fatalf("expected second markInput timestamp %v to be >= first %v", second, first)
		}
	})
}

func TestIsTmuxAvailable(t *testing.T) {
	tests := []struct {
		name      string
		available bool
		want      bool
	}{
		{name: "available", available: true, want: true},
		{name: "unavailable", available: false, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &App{tmuxAvailable: tt.available}
			if got := app.IsTmuxAvailable(); got != tt.want {
				t.Fatalf("IsTmuxAvailable() = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("zero value defaults to false", func(t *testing.T) {
		app := &App{}
		if app.IsTmuxAvailable() {
			t.Fatal("expected IsTmuxAvailable() to be false on a zero-value App")
		}
	})

	t.Run("reflects mutation of the underlying field", func(t *testing.T) {
		app := &App{}
		if app.IsTmuxAvailable() {
			t.Fatal("expected false before flipping the flag")
		}
		app.tmuxAvailable = true
		if !app.IsTmuxAvailable() {
			t.Fatal("expected true after flipping the flag")
		}
	})
}

func TestTmuxSyncWorkspaces(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")

	tests := []struct {
		name      string
		active    *data.Workspace
		wantNil   bool
		wantLen   int
		wantFirst *data.Workspace
	}{
		{
			name:    "nil active workspace returns nil",
			active:  nil,
			wantNil: true,
		},
		{
			name:      "active workspace returns single-element slice",
			active:    ws,
			wantLen:   1,
			wantFirst: ws,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &App{activeWorkspace: tt.active}
			got := app.tmuxSyncWorkspaces()

			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %#v", got)
				}
				return
			}

			if len(got) != tt.wantLen {
				t.Fatalf("expected slice of len %d, got %d", tt.wantLen, len(got))
			}
			if got[0] != tt.wantFirst {
				t.Fatalf("expected first element %p, got %p", tt.wantFirst, got[0])
			}
		})
	}

	t.Run("returns the same pointer as the active workspace", func(t *testing.T) {
		app := &App{activeWorkspace: ws}
		got := app.tmuxSyncWorkspaces()
		if len(got) != 1 {
			t.Fatalf("expected one workspace, got %d", len(got))
		}
		// The returned slice must reference the live active workspace, not a copy,
		// so downstream tmux sync operates on the real object.
		if got[0] != app.activeWorkspace {
			t.Fatal("expected returned workspace to be the same pointer as activeWorkspace")
		}
	})

	t.Run("does not retain the active workspace after it is cleared", func(t *testing.T) {
		app := &App{activeWorkspace: ws}
		if got := app.tmuxSyncWorkspaces(); len(got) != 1 {
			t.Fatalf("expected one workspace while active, got %d", len(got))
		}
		app.activeWorkspace = nil
		if got := app.tmuxSyncWorkspaces(); got != nil {
			t.Fatalf("expected nil after clearing active workspace, got %#v", got)
		}
	})
}
