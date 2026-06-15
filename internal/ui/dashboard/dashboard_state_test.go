package dashboard

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// TestStartSpinnerIfNeeded covers the public StartSpinnerIfNeeded entry point,
// which app-layer callers use to (re)start spinner ticks when work is pending.
// It must only emit a tick command when there is pending create/delete activity
// and the spinner is not already running, and it must flip spinnerActive in step.
func TestStartSpinnerIfNeeded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// setup mutates a fresh Model into the desired starting state.
		setup func(m *Model)
		// wantCmd is whether a (non-nil) tick command should be returned.
		wantCmd bool
		// wantActive is the expected spinnerActive value after the call.
		wantActive bool
	}{
		{
			name:       "no pending activity returns no command",
			setup:      func(m *Model) {},
			wantCmd:    false,
			wantActive: false,
		},
		{
			name: "pending creation starts the spinner",
			setup: func(m *Model) {
				m.creatingWorkspaces["/repo/.amux/workspaces/new"] = &data.Workspace{
					Root: "/repo/.amux/workspaces/new",
				}
			},
			wantCmd:    true,
			wantActive: true,
		},
		{
			name: "pending deletion starts the spinner",
			setup: func(m *Model) {
				m.deletingWorkspaces["/repo/.amux/workspaces/old"] = true
			},
			wantCmd:    true,
			wantActive: true,
		},
		{
			name: "pending creation and deletion starts the spinner",
			setup: func(m *Model) {
				m.creatingWorkspaces["/repo/.amux/workspaces/new"] = &data.Workspace{
					Root: "/repo/.amux/workspaces/new",
				}
				m.deletingWorkspaces["/repo/.amux/workspaces/old"] = true
			},
			wantCmd:    true,
			wantActive: true,
		},
		{
			name: "already active with pending work is idempotent (no new command)",
			setup: func(m *Model) {
				m.spinnerActive = true
				m.creatingWorkspaces["/repo/.amux/workspaces/new"] = &data.Workspace{
					Root: "/repo/.amux/workspaces/new",
				}
			},
			wantCmd:    false,
			wantActive: true,
		},
		{
			name: "already active without pending work stays active and returns no command",
			setup: func(m *Model) {
				m.spinnerActive = true
			},
			wantCmd:    false,
			wantActive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := New()
			tt.setup(m)

			cmd := m.StartSpinnerIfNeeded()

			if gotCmd := cmd != nil; gotCmd != tt.wantCmd {
				t.Errorf("StartSpinnerIfNeeded() returned command=%v, want command=%v", gotCmd, tt.wantCmd)
			}
			if m.spinnerActive != tt.wantActive {
				t.Errorf("spinnerActive = %v, want %v", m.spinnerActive, tt.wantActive)
			}
		})
	}
}

// TestStartSpinnerIfNeededEmittedCommandTicks confirms the returned command is a
// real spinner tick: executing it must yield a SpinnerTickMsg. This guards the
// behavior callers depend on (re-arming the animation loop), not just non-nil.
func TestStartSpinnerIfNeededEmittedCommandTicks(t *testing.T) {
	t.Parallel()

	m := New()
	m.deletingWorkspaces["/repo/.amux/workspaces/old"] = true

	cmd := m.StartSpinnerIfNeeded()
	if cmd == nil {
		t.Fatal("expected a spinner tick command when work is pending")
	}

	msg := cmd()
	if _, ok := msg.(SpinnerTickMsg); !ok {
		t.Fatalf("expected command to produce SpinnerTickMsg, got %T", msg)
	}
}

// TestStartSpinnerIfNeededMatchesUnexported ensures the public wrapper behaves
// identically to the unexported startSpinnerIfNeeded it delegates to, so the
// exported surface can't silently drift from the internal implementation.
func TestStartSpinnerIfNeededMatchesUnexported(t *testing.T) {
	t.Parallel()

	pub := New()
	pub.creatingWorkspaces["/repo/.amux/workspaces/new"] = &data.Workspace{Root: "/repo/.amux/workspaces/new"}
	priv := New()
	priv.creatingWorkspaces["/repo/.amux/workspaces/new"] = &data.Workspace{Root: "/repo/.amux/workspaces/new"}

	pubCmd := pub.StartSpinnerIfNeeded()
	privCmd := priv.startSpinnerIfNeeded()

	if (pubCmd == nil) != (privCmd == nil) {
		t.Fatalf("public/unexported disagree on command: public nil=%v, unexported nil=%v", pubCmd == nil, privCmd == nil)
	}
	if pub.spinnerActive != priv.spinnerActive {
		t.Fatalf("public/unexported disagree on spinnerActive: public=%v, unexported=%v", pub.spinnerActive, priv.spinnerActive)
	}
}

// TestClearActiveRoot covers ClearActiveRoot, which resets the dashboard's
// active workspace selection back to "Home" (the empty activeRoot sentinel).
func TestClearActiveRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		activeRoot string
	}{
		{name: "already home stays home", activeRoot: ""},
		{name: "clears a set workspace root", activeRoot: "/repo/.amux/workspaces/feature"},
		{name: "clears a project root", activeRoot: "/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := New()
			m.activeRoot = tt.activeRoot

			m.ClearActiveRoot()

			if m.activeRoot != "" {
				t.Errorf("ClearActiveRoot() left activeRoot = %q, want empty", m.activeRoot)
			}
		})
	}
}

// TestClearActiveRootIsIdempotent verifies repeated calls keep activeRoot at the
// Home sentinel and don't disturb unrelated state such as projects/rows.
func TestClearActiveRootIsIdempotent(t *testing.T) {
	t.Parallel()

	m := New()
	m.SetProjects([]data.Project{makeProject()})
	m.activeRoot = "/repo/.amux/workspaces/feature"

	rowsBefore := len(m.rows)

	m.ClearActiveRoot()
	m.ClearActiveRoot()

	if m.activeRoot != "" {
		t.Errorf("activeRoot = %q after repeated clears, want empty", m.activeRoot)
	}
	if len(m.rows) != rowsBefore {
		t.Errorf("ClearActiveRoot() mutated rows: got %d, want %d", len(m.rows), rowsBefore)
	}
}
