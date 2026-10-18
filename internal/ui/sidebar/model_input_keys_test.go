package sidebar

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
)

// Key→handler coverage for the Changes bindings that previously only had
// direct method calls: an unbound or remapped key would ship green without
// these. Each test drives Update with the real key message and asserts the
// emitted command's message (or the observable state flip for `b`, whose
// cmd would shell git if executed).
//
// Keys still covered elsewhere: j/k/arrows, enter/space/o, v, g, c, e, O,
// i, `/` (model_input_test.go, filter_input_test.go).

func keyedWorkspaceModel(t *testing.T) (*ChangesModel, *data.Workspace) {
	t.Helper()
	m := newInputModel(t)
	ws := &data.Workspace{Name: "ws", Root: "/repo/ws"}
	m.SetWorkspace(ws)
	return m, ws
}

func TestInputB_TogglesBranchMode(t *testing.T) {
	m, _ := keyedWorkspaceModel(t)
	if m.branchMode {
		t.Fatal("fixture unexpectedly started in branch mode")
	}

	_, cmd := m.Update(keyPress('b'))
	if !m.branchMode {
		t.Fatal("b did not enter branch mode")
	}
	if !m.branchLoading {
		t.Fatal("b did not mark the branch list loading")
	}
	if cmd == nil {
		t.Fatal("b with a workspace should emit a branch-changes load cmd")
	}
	// Do not execute cmd — loadBranchChanges shells git.BranchChangesVsBase.

	m2, _ := m.Update(keyPress('b'))
	if m2.branchMode {
		t.Fatal("second b did not leave branch mode")
	}
}

func TestInputE_EmitsShowProjectEnvDialog(t *testing.T) {
	m, ws := keyedWorkspaceModel(t)

	_, cmd := m.Update(keyPress('E'))
	if cmd == nil {
		t.Fatal("expected non-nil cmd from E")
	}
	show, ok := cmd().(messages.ShowProjectEnvDialog)
	if !ok {
		t.Fatalf("expected messages.ShowProjectEnvDialog, got %T", cmd())
	}
	if show.Workspace != ws {
		t.Fatalf("ShowProjectEnvDialog workspace = %p, want %p", show.Workspace, ws)
	}
}

func TestInputS_EmitsShowWorkspaceScriptsDialog(t *testing.T) {
	m, ws := keyedWorkspaceModel(t)

	_, cmd := m.Update(keyPress('s'))
	if cmd == nil {
		t.Fatal("expected non-nil cmd from s")
	}
	show, ok := cmd().(messages.ShowWorkspaceScriptsDialog)
	if !ok {
		t.Fatalf("expected messages.ShowWorkspaceScriptsDialog, got %T", cmd())
	}
	if show.Workspace != ws {
		t.Fatalf("ShowWorkspaceScriptsDialog workspace = %p, want %p", show.Workspace, ws)
	}
}

func TestInputR_EmitsToggleWorkspaceScript(t *testing.T) {
	m, ws := keyedWorkspaceModel(t)

	_, cmd := m.Update(keyPress('r'))
	if cmd == nil {
		t.Fatal("expected non-nil cmd from r")
	}
	show, ok := cmd().(messages.ToggleWorkspaceScript)
	if !ok {
		t.Fatalf("expected messages.ToggleWorkspaceScript, got %T", cmd())
	}
	if show.Workspace != ws {
		t.Fatalf("ToggleWorkspaceScript workspace = %p, want %p", show.Workspace, ws)
	}
}

func TestInputShiftR_EmitsShowRunScriptOutput(t *testing.T) {
	m, ws := keyedWorkspaceModel(t)

	_, cmd := m.Update(keyPress('R'))
	if cmd == nil {
		t.Fatal("expected non-nil cmd from R")
	}
	show, ok := cmd().(messages.ShowRunScriptOutput)
	if !ok {
		t.Fatalf("expected messages.ShowRunScriptOutput, got %T", cmd())
	}
	if show.Workspace != ws {
		t.Fatalf("ShowRunScriptOutput workspace = %p, want %p", show.Workspace, ws)
	}
}

func TestInputU_EmitsRerunWorkspaceScript(t *testing.T) {
	m, ws := keyedWorkspaceModel(t)

	_, cmd := m.Update(keyPress('u'))
	if cmd == nil {
		t.Fatal("expected non-nil cmd from u")
	}
	show, ok := cmd().(messages.RerunWorkspaceScript)
	if !ok {
		t.Fatalf("expected messages.RerunWorkspaceScript, got %T", cmd())
	}
	if show.Workspace != ws {
		t.Fatalf("RerunWorkspaceScript workspace = %p, want %p", show.Workspace, ws)
	}
	if show.Script != process.ScriptSetup {
		t.Fatalf("RerunWorkspaceScript script = %q, want %q", show.Script, process.ScriptSetup)
	}
}

// TestInputKeysNoWorkspace_NoOp pins the other half of the binding contract:
// every workspace-gated key must no-op cleanly (not panic, not emit) when no
// workspace is focused.
func TestInputKeysNoWorkspace_NoOp(t *testing.T) {
	for _, k := range []rune{'E', 's', 'r', 'R', 'u', 'e', 'i', 'O', 'c'} {
		m := newInputModel(t) // workspace intentionally unset
		_, cmd := m.Update(keyPress(k))
		if cmd == nil {
			continue
		}
		if msg := cmd(); msg != nil {
			t.Fatalf("key %q with no workspace emitted %T; want silent no-op", string(k), msg)
		}
	}
}
