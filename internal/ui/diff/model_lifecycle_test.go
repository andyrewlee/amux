package diff

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// TestNew asserts the constructor wires every field and seeds the loading +
// default-styles state a freshly opened viewer expects.
func TestNew(t *testing.T) {
	ws := data.NewWorkspace("ws", "ws", "main", "/repo", "/repo")
	change := &git.Change{Path: "main.go", Kind: git.ChangeModified}

	m := New(ws, change, git.DiffModeStaged, 120, 40)

	if m == nil {
		t.Fatal("New returned nil model")
	}
	if m.workspace != ws {
		t.Errorf("workspace = %p, want %p", m.workspace, ws)
	}
	if m.change != change {
		t.Errorf("change = %p, want %p", m.change, change)
	}
	if m.mode != git.DiffModeStaged {
		t.Errorf("mode = %v, want DiffModeStaged", m.mode)
	}
	if m.width != 120 {
		t.Errorf("width = %d, want 120", m.width)
	}
	if m.height != 40 {
		t.Errorf("height = %d, want 40", m.height)
	}
	if !m.loading {
		t.Error("expected new model to start in loading state")
	}
	if m.focused {
		t.Error("expected new model to start unfocused")
	}
	// DefaultStyles seeds a non-zero style set; compare against the package
	// default to confirm SetStyles was effectively applied at construction.
	if m.styles.Body.String() != common.DefaultStyles().Body.String() {
		t.Error("expected new model to use DefaultStyles")
	}
}

// TestNew_NilInputs proves the constructor tolerates nil workspace/change and
// zero dimensions without panicking, since callers build a viewer before any
// file is selected.
func TestNew_NilInputs(t *testing.T) {
	m := New(nil, nil, git.DiffModeUnstaged, 0, 0)
	if m == nil {
		t.Fatal("New returned nil model for nil inputs")
	}
	if m.workspace != nil || m.change != nil {
		t.Error("expected nil workspace/change to be preserved")
	}
	if !m.loading {
		t.Error("expected loading state even with nil inputs")
	}
	if got := m.GetPath(); got != "" {
		t.Errorf("GetPath on nil change = %q, want empty", got)
	}
}

// TestInit returns a non-nil load command and bumps the load generation so the
// first async result is accepted by Update.
func TestInit(t *testing.T) {
	m := New(nil, nil, git.DiffModeUnstaged, 80, 24)
	startID := m.loadID

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned nil command")
	}
	if m.loadID != startID+1 {
		t.Fatalf("Init loadID = %d, want %d", m.loadID, startID+1)
	}
}

// TestInitNilInputsProducesEmptyDiff executes the command returned by Init for a
// viewer with no workspace/change. That branch short-circuits before touching
// the git CLI, so it is safe to run and must yield an empty-diff completion that
// carries the current load generation.
func TestInitNilInputsProducesEmptyDiff(t *testing.T) {
	m := New(nil, nil, git.DiffModeUnstaged, 80, 24)

	cmd := m.Init()
	msg := cmd()

	loaded, ok := msg.(diffLoaded)
	if !ok {
		t.Fatalf("Init command produced %T, want diffLoaded", msg)
	}
	if loaded.err != nil {
		t.Errorf("nil-input load err = %v, want nil", loaded.err)
	}
	if loaded.diff == nil || !loaded.diff.Empty {
		t.Errorf("nil-input load diff = %+v, want Empty result", loaded.diff)
	}
	if loaded.loadID != m.loadID {
		t.Errorf("nil-input load loadID = %d, want %d", loaded.loadID, m.loadID)
	}

	// Feeding the result back through Update must clear loading and record the
	// empty diff under the matching generation.
	updated, _ := m.Update(loaded)
	if updated.loading {
		t.Error("expected nil-input load to clear loading state")
	}
	if updated.diff == nil || !updated.diff.Empty {
		t.Errorf("expected empty diff recorded, got %+v", updated.diff)
	}
}

// TestLoadDiffIncrementsLoadID confirms each load bumps the generation counter
// and hands back an executable command, the contract Update relies on to drop
// stale completions.
func TestLoadDiffIncrementsLoadID(t *testing.T) {
	m := New(nil, nil, git.DiffModeUnstaged, 80, 24)

	first := m.loadID
	cmd1 := m.loadDiff()
	if cmd1 == nil {
		t.Fatal("loadDiff returned nil command")
	}
	if m.loadID != first+1 {
		t.Fatalf("first loadDiff loadID = %d, want %d", m.loadID, first+1)
	}

	cmd2 := m.loadDiff()
	if cmd2 == nil {
		t.Fatal("second loadDiff returned nil command")
	}
	if m.loadID != first+2 {
		t.Fatalf("second loadDiff loadID = %d, want %d", m.loadID, first+2)
	}

	// Each closure must capture its own generation: executing the first (nil
	// inputs, no git CLI) carries the generation that was current when it was
	// created, not the latest.
	loaded, ok := cmd1().(diffLoaded)
	if !ok {
		t.Fatalf("loadDiff command produced %T, want diffLoaded", cmd1())
	}
	if loaded.loadID != first+1 {
		t.Errorf("captured loadID = %d, want %d", loaded.loadID, first+1)
	}
}

// TestLoadDiffNonNilInputsReturnsCommand verifies that with a real workspace and
// change the loader still bumps the generation and returns a runnable command.
// The command is intentionally not executed here because doing so shells out to
// the git CLI; the executed git paths live in the git package's own tests.
func TestLoadDiffNonNilInputsReturnsCommand(t *testing.T) {
	ws := data.NewWorkspace("ws", "ws", "main", "/repo", "/repo")
	change := &git.Change{Path: "main.go", Kind: git.ChangeModified}
	m := New(ws, change, git.DiffModeUnstaged, 80, 24)

	before := m.loadID
	if cmd := m.loadDiff(); cmd == nil {
		t.Fatal("loadDiff returned nil command for populated model")
	}
	if m.loadID != before+1 {
		t.Fatalf("loadDiff loadID = %d, want %d", m.loadID, before+1)
	}
}
