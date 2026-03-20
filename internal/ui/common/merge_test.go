package common

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestRebindWorkspaceNilCurrent(t *testing.T) {
	existing := data.NewWorkspace("a", "a", "main", "/repo", "/repo")
	got := RebindWorkspace(nil, existing, false)
	if got != existing {
		t.Fatal("expected existing workspace when current is nil")
	}
}

func TestRebindWorkspaceNilExisting(t *testing.T) {
	current := data.NewWorkspace("a", "a", "main", "/repo", "/repo")
	got := RebindWorkspace(current, nil, true)
	if got != current {
		t.Fatal("expected current workspace when existing is nil")
	}
}

func TestRebindWorkspacePreservesRuntime(t *testing.T) {
	current := data.NewWorkspace("a", "a", "main", "/repo", "/repo")
	current.Runtime = data.RuntimeLocalWorktree
	existing := data.NewWorkspace("a", "a", "main", "/repo", "/repo")
	existing.Runtime = data.RuntimeCloudSandbox

	got := RebindWorkspace(current, existing, true)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if data.NormalizeRuntime(got.Runtime) != data.RuntimeCloudSandbox {
		t.Fatalf("runtime = %q, want %q", got.Runtime, data.RuntimeCloudSandbox)
	}
}

func TestRebindWorkspaceNoPreserve(t *testing.T) {
	current := data.NewWorkspace("a", "a", "main", "/repo", "/repo")
	current.Runtime = data.RuntimeLocalWorktree
	existing := data.NewWorkspace("a", "a", "main", "/repo", "/repo")
	existing.Runtime = data.RuntimeCloudSandbox

	got := RebindWorkspace(current, existing, false)
	if got != current {
		t.Fatal("expected current workspace when not preserving runtime")
	}
}

type testItem struct {
	id   string
	name string
}

func TestMergeByIDBasic(t *testing.T) {
	existing := []*testItem{{id: "a", name: "A"}, {id: "b", name: "B"}}
	incoming := []*testItem{{id: "b", name: "B2"}, {id: "c", name: "C"}}

	merged, active := MergeByID(existing, incoming, 1,
		func(t *testItem) string { return t.id },
		func(t *testItem) bool { return t == nil },
	)

	if len(merged) != 3 {
		t.Fatalf("merged len = %d, want 3", len(merged))
	}
	if merged[0].id != "a" || merged[1].id != "b" || merged[2].id != "c" {
		t.Fatalf("unexpected merge order: %v", merged)
	}
	// incoming active was index 1 ("c"), which maps to merged index 2
	if active != 2 {
		t.Fatalf("migratedActive = %d, want 2", active)
	}
}

func TestMergeByIDDuplicateActive(t *testing.T) {
	existing := []*testItem{{id: "x", name: "X"}}
	incoming := []*testItem{{id: "x", name: "X2"}}

	_, active := MergeByID(existing, incoming, 0,
		func(t *testItem) string { return t.id },
		func(t *testItem) bool { return t == nil },
	)

	// incoming active was "x" at index 0, which already exists at merged index 0
	if active != 0 {
		t.Fatalf("migratedActive = %d, want 0", active)
	}
}

func TestMergeByIDNilItems(t *testing.T) {
	existing := []*testItem{nil, {id: "a", name: "A"}}
	incoming := []*testItem{{id: "b", name: "B"}, nil}

	merged, _ := MergeByID(existing, incoming, -1,
		func(t *testItem) string { return t.id },
		func(t *testItem) bool { return t == nil },
	)

	if len(merged) != 2 {
		t.Fatalf("merged len = %d, want 2", len(merged))
	}
}

func TestMergeByIDEmpty(t *testing.T) {
	merged, active := MergeByID([]*testItem{}, []*testItem{}, 0,
		func(t *testItem) string { return t.id },
		func(t *testItem) bool { return t == nil },
	)
	if len(merged) != 0 {
		t.Fatalf("merged len = %d, want 0", len(merged))
	}
	if active != -1 {
		t.Fatalf("migratedActive = %d, want -1", active)
	}
}
