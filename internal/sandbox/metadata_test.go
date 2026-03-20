package sandbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestComputeWorktreeID(t *testing.T) {
	tests := []struct {
		name     string
		cwd      string
		wantLen  int
		wantSame bool // if true, compare with previous test
	}{
		{
			name:    "absolute path",
			cwd:     "/home/user/project",
			wantLen: 16,
		},
		{
			name:    "different path gives different ID",
			cwd:     "/home/user/other-project",
			wantLen: 16,
		},
		{
			name:    "same path gives same ID",
			cwd:     "/home/user/project",
			wantLen: 16,
		},
	}

	var prevID string
	var firstID string

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeWorktreeID(tt.cwd)

			if len(got) != tt.wantLen {
				t.Errorf("ComputeWorktreeID() length = %d, want %d", len(got), tt.wantLen)
			}

			// Verify it's a valid hex string
			for _, c := range got {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
					t.Errorf("ComputeWorktreeID() contains invalid hex char: %c", c)
				}
			}

			// Track IDs for comparison
			if i == 0 {
				firstID = got
			} else if i == 1 {
				// Second test should be different from first
				if got == firstID {
					t.Error("Different paths should produce different IDs")
				}
				prevID = got
			} else if i == 2 {
				// Third test (same path as first) should match first
				if got != firstID {
					t.Error("Same path should produce same ID")
				}
				// And be different from second
				if got == prevID {
					t.Error("Same path should not match different path's ID")
				}
			}
		})
	}
}

func TestComputeWorktreeID_Deterministic(t *testing.T) {
	cwd := "/home/user/my-project"

	// Call multiple times, should always return same value
	id1 := ComputeWorktreeID(cwd)
	id2 := ComputeWorktreeID(cwd)
	id3 := ComputeWorktreeID(cwd)

	if id1 != id2 || id2 != id3 {
		t.Errorf("ComputeWorktreeID should be deterministic: got %s, %s, %s", id1, id2, id3)
	}
}

func TestMoveSandboxMetaPreservesEntryForSameWorktreeID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repo := t.TempDir()
	t.Chdir(repo)

	needsSync := true
	if err := SaveSandboxMeta(".", "fake", SandboxMeta{
		SandboxID:     "sb-same-worktree",
		Agent:         AgentShell,
		Provider:      "fake",
		NeedsSyncDown: &needsSync,
	}); err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}

	absRepo, err := filepath.Abs(repo)
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}

	if ComputeWorktreeID(".") != ComputeWorktreeID(absRepo) {
		t.Fatalf("expected relative and absolute paths to share worktree ID")
	}
	if err := MoveSandboxMeta(".", absRepo, "fake"); err != nil {
		t.Fatalf("MoveSandboxMeta() error = %v", err)
	}

	meta, err := LoadSandboxMeta(absRepo, "fake")
	if err != nil {
		t.Fatalf("LoadSandboxMeta() error = %v", err)
	}
	if meta == nil || meta.SandboxID != "sb-same-worktree" {
		t.Fatalf("LoadSandboxMeta() = %#v, want sandbox metadata to remain present", meta)
	}
}

func TestLoadSandboxMetaFallsBackAcrossEquivalentPathAliases(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	realRepo := filepath.Join(t.TempDir(), "repo-real")
	if err := os.MkdirAll(realRepo, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", realRepo, err)
	}
	linkRepo := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(realRepo, linkRepo); err != nil {
		t.Fatalf("Symlink(%q -> %q) error = %v", linkRepo, realRepo, err)
	}

	if err := SaveSandboxMeta(linkRepo, "fake", SandboxMeta{
		SandboxID: "sb-alias-fallback",
		Agent:     AgentShell,
		Provider:  "fake",
	}); err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}

	meta, err := LoadSandboxMeta(realRepo, "fake")
	if err != nil {
		t.Fatalf("LoadSandboxMeta() error = %v", err)
	}
	if meta == nil || meta.SandboxID != "sb-alias-fallback" {
		t.Fatalf("LoadSandboxMeta() = %#v, want alias metadata", meta)
	}
	if meta.WorkspaceRoot != canonicalSandboxMetaPath(realRepo) {
		t.Fatalf("WorkspaceRoot = %q, want canonical root %q", meta.WorkspaceRoot, canonicalSandboxMetaPath(realRepo))
	}
}

func TestLoadSandboxMetaFallbackIsNilWhenCanonicalMatchIsAmbiguousAcrossProviders(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	realRepo := filepath.Join(t.TempDir(), "repo-real")
	if err := os.MkdirAll(realRepo, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", realRepo, err)
	}
	linkRepoA := filepath.Join(t.TempDir(), "repo-link-a")
	if err := os.Symlink(realRepo, linkRepoA); err != nil {
		t.Fatalf("Symlink(%q -> %q) error = %v", linkRepoA, realRepo, err)
	}
	linkRepoB := filepath.Join(t.TempDir(), "repo-link-b")
	if err := os.Symlink(realRepo, linkRepoB); err != nil {
		t.Fatalf("Symlink(%q -> %q) error = %v", linkRepoB, realRepo, err)
	}

	if err := SaveSandboxMeta(linkRepoA, "provider-a", SandboxMeta{
		SandboxID: "sb-provider-a",
		Agent:     AgentShell,
		Provider:  "provider-a",
	}); err != nil {
		t.Fatalf("SaveSandboxMeta(provider-a) error = %v", err)
	}
	if err := SaveSandboxMeta(linkRepoB, "provider-b", SandboxMeta{
		SandboxID: "sb-provider-b",
		Agent:     AgentShell,
		Provider:  "provider-b",
	}); err != nil {
		t.Fatalf("SaveSandboxMeta(provider-b) error = %v", err)
	}

	requestedRepo := filepath.Join(t.TempDir(), "repo-requested")
	if err := os.Symlink(realRepo, requestedRepo); err != nil {
		t.Fatalf("Symlink(%q -> %q) error = %v", requestedRepo, realRepo, err)
	}

	meta, err := LoadSandboxMeta(requestedRepo, "")
	if err != nil {
		t.Fatalf("LoadSandboxMeta() error = %v", err)
	}
	if meta != nil {
		t.Fatalf("LoadSandboxMeta() = %#v, want nil for ambiguous providerless canonical fallback", meta)
	}
}

func TestLoadSandboxMetaFallsBackAfterExactKeyProviderMismatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	realRepo := filepath.Join(t.TempDir(), "repo-real")
	if err := os.MkdirAll(realRepo, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", realRepo, err)
	}
	linkRepo := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(realRepo, linkRepo); err != nil {
		t.Fatalf("Symlink(%q -> %q) error = %v", linkRepo, realRepo, err)
	}

	if err := SaveSandboxMeta(realRepo, "provider-a", SandboxMeta{
		SandboxID: "sb-provider-a",
		Agent:     AgentShell,
		Provider:  "provider-a",
	}); err != nil {
		t.Fatalf("SaveSandboxMeta(provider-a) error = %v", err)
	}
	if err := SaveSandboxMeta(linkRepo, "provider-b", SandboxMeta{
		SandboxID: "sb-provider-b",
		Agent:     AgentShell,
		Provider:  "provider-b",
	}); err != nil {
		t.Fatalf("SaveSandboxMeta(provider-b) error = %v", err)
	}

	meta, err := LoadSandboxMeta(realRepo, "provider-b")
	if err != nil {
		t.Fatalf("LoadSandboxMeta() error = %v", err)
	}
	if meta == nil || meta.SandboxID != "sb-provider-b" {
		t.Fatalf("LoadSandboxMeta() = %#v, want provider-b alias metadata", meta)
	}
	if meta.Provider != "provider-b" {
		t.Fatalf("Provider = %q, want %q", meta.Provider, "provider-b")
	}
}

func TestLoadSandboxMetaPrefersExactProviderOverLegacyBlankProviderEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	realRepo := filepath.Join(t.TempDir(), "repo-real")
	if err := os.MkdirAll(realRepo, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", realRepo, err)
	}
	linkRepo := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(realRepo, linkRepo); err != nil {
		t.Fatalf("Symlink(%q -> %q) error = %v", linkRepo, realRepo, err)
	}

	store := SandboxStore{
		Sandboxes: map[string]SandboxMeta{
			ComputeWorktreeID(realRepo): {
				SandboxID:     "sb-legacy",
				Agent:         AgentShell,
				WorkspaceRoot: realRepo,
			},
			ComputeWorktreeID(linkRepo): {
				SandboxID:     "sb-provider-b",
				Agent:         AgentShell,
				Provider:      "provider-b",
				WorkspaceRoot: linkRepo,
			},
		},
	}
	metaPath, err := globalMetaPath()
	if err != nil {
		t.Fatalf("globalMetaPath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(meta dir) error = %v", err)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent(store) error = %v", err)
	}
	if err := os.WriteFile(metaPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile(metaPath) error = %v", err)
	}

	meta, err := LoadSandboxMeta(realRepo, "provider-b")
	if err != nil {
		t.Fatalf("LoadSandboxMeta() error = %v", err)
	}
	if meta == nil || meta.SandboxID != "sb-provider-b" {
		t.Fatalf("LoadSandboxMeta() = %#v, want provider-specific metadata", meta)
	}
	if meta.Provider != "provider-b" {
		t.Fatalf("Provider = %q, want %q", meta.Provider, "provider-b")
	}
}

func TestLoadSandboxStoreTreatsMalformedFileAsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	metaPath, err := globalMetaPath()
	if err != nil {
		t.Fatalf("globalMetaPath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(meta dir) error = %v", err)
	}
	if err := os.WriteFile(metaPath, []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile(metaPath) error = %v", err)
	}

	store, err := LoadSandboxStore()
	if err != nil {
		t.Fatalf("LoadSandboxStore() error = %v", err)
	}
	if store == nil {
		t.Fatal("LoadSandboxStore() = nil, want empty store")
	}
	if len(store.Sandboxes) != 0 {
		t.Fatalf("LoadSandboxStore() sandboxes = %v, want empty", store.Sandboxes)
	}
}

func TestSaveSandboxMetaRecoversFromMalformedStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repo := t.TempDir()
	metaPath, err := globalMetaPath()
	if err != nil {
		t.Fatalf("globalMetaPath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(meta dir) error = %v", err)
	}
	if err := os.WriteFile(metaPath, []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile(metaPath) error = %v", err)
	}

	if err := SaveSandboxMeta(repo, "fake", SandboxMeta{
		SandboxID: "sb-recovered-from-malformed-store",
		Agent:     AgentShell,
		Provider:  "fake",
	}); err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}

	meta, err := LoadSandboxMeta(repo, "fake")
	if err != nil {
		t.Fatalf("LoadSandboxMeta() error = %v", err)
	}
	if meta == nil || meta.SandboxID != "sb-recovered-from-malformed-store" {
		t.Fatalf("LoadSandboxMeta() = %#v, want recovered metadata", meta)
	}
}

func TestLoadSandboxMetaFallbackSurvivesRelativeWorkspaceRootAcrossCwdChange(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", repo, err)
	}
	other := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", other, err)
	}

	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalCwd) })

	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(%q) error = %v", repo, err)
	}
	if err := SaveSandboxMeta(".", "fake", SandboxMeta{
		SandboxID: "sb-relative-root",
		Agent:     AgentShell,
		Provider:  "fake",
	}); err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}

	if err := os.Chdir(other); err != nil {
		t.Fatalf("Chdir(%q) error = %v", other, err)
	}
	meta, err := LoadSandboxMeta(repo, "fake")
	if err != nil {
		t.Fatalf("LoadSandboxMeta() error = %v", err)
	}
	if meta == nil || meta.SandboxID != "sb-relative-root" {
		t.Fatalf("LoadSandboxMeta() = %#v, want relative-root metadata", meta)
	}
	if meta.WorkspaceRoot != canonicalSandboxMetaPath(repo) {
		t.Fatalf("WorkspaceRoot = %q, want %q", meta.WorkspaceRoot, canonicalSandboxMetaPath(repo))
	}
}

func TestLoadSandboxMetaFallbackRestoresLegacyWorktreeIDFromStoredKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	realRepo := filepath.Join(t.TempDir(), "repo-real")
	if err := os.MkdirAll(realRepo, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", realRepo, err)
	}
	linkRepo := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(realRepo, linkRepo); err != nil {
		t.Fatalf("Symlink(%q -> %q) error = %v", linkRepo, realRepo, err)
	}

	store := SandboxStore{
		Sandboxes: map[string]SandboxMeta{
			ComputeWorktreeID(linkRepo): {
				SandboxID:     "sb-legacy-alias",
				Agent:         AgentShell,
				Provider:      "fake",
				WorkspaceRoot: linkRepo,
			},
		},
	}
	metaPath, err := globalMetaPath()
	if err != nil {
		t.Fatalf("globalMetaPath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(meta dir) error = %v", err)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent() error = %v", err)
	}
	if err := os.WriteFile(metaPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile(meta) error = %v", err)
	}

	meta, err := LoadSandboxMeta(realRepo, "fake")
	if err != nil {
		t.Fatalf("LoadSandboxMeta() error = %v", err)
	}
	if meta == nil || meta.SandboxID != "sb-legacy-alias" {
		t.Fatalf("LoadSandboxMeta() = %#v, want legacy alias metadata", meta)
	}
	if meta.WorktreeID != ComputeWorktreeID(linkRepo) {
		t.Fatalf("WorktreeID = %q, want stored key %q", meta.WorktreeID, ComputeWorktreeID(linkRepo))
	}
}

func TestLoadSandboxMetaSkipsSingleLegacyEntryWithoutWorkspaceRootForUnmatchedWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	storedRepo := filepath.Join(t.TempDir(), "legacy-workspace")
	if err := os.MkdirAll(storedRepo, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", storedRepo, err)
	}
	requestedRepo := filepath.Join(t.TempDir(), "requested-workspace")
	if err := os.MkdirAll(requestedRepo, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", requestedRepo, err)
	}

	store := SandboxStore{
		Sandboxes: map[string]SandboxMeta{
			ComputeWorktreeID(storedRepo): {
				SandboxID: "sb-legacy-no-root-unrelated",
				Agent:     AgentShell,
				Provider:  "fake",
			},
		},
	}
	metaPath, err := globalMetaPath()
	if err != nil {
		t.Fatalf("globalMetaPath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(meta dir) error = %v", err)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent() error = %v", err)
	}
	if err := os.WriteFile(metaPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile(meta) error = %v", err)
	}

	meta, err := LoadSandboxMeta(requestedRepo, "fake")
	if err != nil {
		t.Fatalf("LoadSandboxMeta() error = %v", err)
	}
	if meta != nil {
		t.Fatalf("LoadSandboxMeta() = %#v, want nil for unrelated workspace", meta)
	}
}

func TestMoveSandboxMetaUpdatesWorkspaceRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	oldRoot := filepath.Join(t.TempDir(), "repo-old")
	newRoot := filepath.Join(t.TempDir(), "repo-new")
	if err := os.MkdirAll(oldRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", oldRoot, err)
	}
	if err := os.MkdirAll(newRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", newRoot, err)
	}

	if err := SaveSandboxMeta(oldRoot, "fake", SandboxMeta{
		SandboxID: "sb-move-root",
		Agent:     AgentShell,
		Provider:  "fake",
	}); err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}
	if err := MoveSandboxMeta(oldRoot, newRoot, "fake"); err != nil {
		t.Fatalf("MoveSandboxMeta() error = %v", err)
	}

	meta, err := LoadSandboxMeta(newRoot, "fake")
	if err != nil {
		t.Fatalf("LoadSandboxMeta() error = %v", err)
	}
	if meta == nil || meta.SandboxID != "sb-move-root" {
		t.Fatalf("LoadSandboxMeta() = %#v, want moved metadata", meta)
	}
	if meta.WorkspaceRoot != canonicalSandboxMetaPath(newRoot) {
		t.Fatalf("WorkspaceRoot = %q, want %q", meta.WorkspaceRoot, canonicalSandboxMetaPath(newRoot))
	}
}
