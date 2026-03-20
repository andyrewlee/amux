package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/sandbox"
)

func TestPersistPendingSyncTargetUsesSelectedProviderForMetadataLookup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AMUX_PROVIDER", "provider-b")

	base := t.TempDir()
	realRepo := filepath.Join(base, "repo-real")
	realRoot := filepath.Join(realRepo, "feature")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", realRoot, err)
	}

	linkRepoA := filepath.Join(base, "repo-link-a")
	if err := os.Symlink(realRepo, linkRepoA); err != nil {
		t.Fatalf("Symlink(%q -> %q) error = %v", linkRepoA, realRepo, err)
	}
	linkRootA := filepath.Join(linkRepoA, "feature")

	linkRepoB := filepath.Join(base, "repo-link-b")
	if err := os.Symlink(realRepo, linkRepoB); err != nil {
		t.Fatalf("Symlink(%q -> %q) error = %v", linkRepoB, realRepo, err)
	}
	linkRootB := filepath.Join(linkRepoB, "feature")

	currentRepo := filepath.Join(base, "repo-current")
	currentRoot := filepath.Join(currentRepo, "feature")
	if err := os.MkdirAll(currentRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", currentRoot, err)
	}

	previous := data.NewWorkspace("feature", "main", "main", linkRepoB, linkRootB)
	current := data.NewWorkspace("feature", "main", "main", currentRepo, currentRoot)
	if previous.ID() == current.ID() {
		t.Fatalf("expected distinct workspace IDs for alias and canonical roots, both were %q", previous.ID())
	}

	if err := sandbox.SaveSandboxMeta(linkRootA, "provider-a", sandbox.SandboxMeta{
		SandboxID:    "sb-provider-a",
		Agent:        sandbox.AgentShell,
		Provider:     "provider-a",
		WorktreeID:   sandbox.ComputeWorktreeID(linkRootA),
		WorkspaceIDs: []string{string(previous.ID())},
	}); err != nil {
		t.Fatalf("SaveSandboxMeta(provider-a) error = %v", err)
	}
	if err := sandbox.SaveSandboxMeta(realRoot, "provider-b", sandbox.SandboxMeta{
		SandboxID:    "sb-provider-b",
		Agent:        sandbox.AgentShell,
		Provider:     "provider-b",
		WorktreeID:   sandbox.ComputeWorktreeID(realRoot),
		WorkspaceIDs: []string{string(previous.ID())},
	}); err != nil {
		t.Fatalf("SaveSandboxMeta(provider-b) error = %v", err)
	}

	origLoadMeta := loadSandboxMeta
	t.Cleanup(func() {
		loadSandboxMeta = origLoadMeta
	})
	loadSandboxMeta = func(cwd, providerName string) (*sandbox.SandboxMeta, error) {
		if providerName != "provider-b" {
			t.Fatalf("metadata provider filter = %q, want %q", providerName, "provider-b")
		}
		return sandbox.LoadSandboxMeta(cwd, providerName)
	}

	manager := NewSandboxManager(nil)
	if err := manager.PersistPendingSyncTarget(previous, current); err != nil {
		t.Fatalf("PersistPendingSyncTarget() error = %v", err)
	}

	metaB, err := sandbox.LoadSandboxMeta(current.Root, "provider-b")
	if err != nil {
		t.Fatalf("LoadSandboxMeta(provider-b) error = %v", err)
	}
	if metaB == nil || metaB.SandboxID != "sb-provider-b" {
		t.Fatalf("provider-b metadata = %#v, want active provider metadata", metaB)
	}
	if !containsWorkspaceID(metaB.WorkspaceIDs, string(current.ID())) {
		t.Fatalf("provider-b workspace IDs = %v, want %q included", metaB.WorkspaceIDs, current.ID())
	}

	metaA, err := sandbox.LoadSandboxMeta(linkRootA, "provider-a")
	if err != nil {
		t.Fatalf("LoadSandboxMeta(provider-a) error = %v", err)
	}
	if metaA == nil || metaA.SandboxID != "sb-provider-a" {
		t.Fatalf("provider-a metadata = %#v, want stale provider metadata untouched", metaA)
	}
	if containsWorkspaceID(metaA.WorkspaceIDs, string(current.ID())) {
		t.Fatalf("provider-a workspace IDs = %v, did not expect %q", metaA.WorkspaceIDs, current.ID())
	}
}

func containsWorkspaceID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
