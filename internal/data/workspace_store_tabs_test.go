package data

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestWorkspaceStoreAppendOpenTab(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	ws := NewWorkspace("ws-a", "main", "origin/main", "/repo", "/repo/ws-a")
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	tab := TabInfo{
		Assistant:   "claude",
		Name:        "claude",
		SessionName: "session-a",
		Status:      "running",
		CreatedAt:   time.Now().Unix(),
	}
	if err := store.AppendOpenTab(ws.ID(), tab); err != nil {
		t.Fatalf("AppendOpenTab() error = %v", err)
	}

	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.OpenTabs) != 1 {
		t.Fatalf("open tabs = %d, want 1", len(loaded.OpenTabs))
	}
	if loaded.OpenTabs[0].SessionName != "session-a" {
		t.Fatalf("session_name = %q, want %q", loaded.OpenTabs[0].SessionName, "session-a")
	}
}

func TestWorkspaceStoreAppendOpenTabDedupesSessionName(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	ws := NewWorkspace("ws-a", "main", "origin/main", "/repo", "/repo/ws-a")
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	tab := TabInfo{
		Assistant:   "claude",
		Name:        "claude",
		SessionName: "session-a",
		Status:      "running",
		CreatedAt:   time.Now().Unix(),
	}
	if err := store.AppendOpenTab(ws.ID(), tab); err != nil {
		t.Fatalf("AppendOpenTab(first) error = %v", err)
	}
	if err := store.AppendOpenTab(ws.ID(), tab); err != nil {
		t.Fatalf("AppendOpenTab(second) error = %v", err)
	}

	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.OpenTabs) != 1 {
		t.Fatalf("open tabs = %d, want 1", len(loaded.OpenTabs))
	}
}

// TestWorkspaceStoreSaveLockedStaleTmpDoesNotShadowCommitted is a
// characterization test for the locked save path (Update/AppendOpenTab). It
// pins the invariant that a leftover <id>/workspace.json.tmp from a crashed
// write never shadows the committed workspace.json: the committed metadata must
// still load. This holds both before and after routing the write through
// fsatomic (fsatomic uses a randomized temp name and fsync, so the safety only
// improves).
func TestWorkspaceStoreSaveLockedStaleTmpDoesNotShadowCommitted(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	ws := NewWorkspace("ws-a", "main", "origin/main", "/repo", "/repo/ws-a")
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Write through the locked path so the committed file is produced by it.
	tab := TabInfo{
		Assistant:   "claude",
		Name:        "claude",
		SessionName: "session-a",
		Status:      "running",
		CreatedAt:   time.Now().Unix(),
	}
	if err := store.AppendOpenTab(ws.ID(), tab); err != nil {
		t.Fatalf("AppendOpenTab() error = %v", err)
	}
	if err := store.Update(ws.ID(), func(w *Workspace) error {
		w.Branch = "feature"
		return nil
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	// Plant a stale, truncated .tmp artifact next to the committed file, as a
	// crashed write would leave behind.
	committedPath := store.workspacePath(ws.ID())
	stalePath := committedPath + ".tmp"
	if err := os.WriteFile(stalePath, []byte("{truncated"), 0o644); err != nil {
		t.Fatalf("plant stale tmp: %v", err)
	}

	// The committed metadata must still load — the stale .tmp must not shadow it.
	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Branch != "feature" {
		t.Fatalf("Branch = %q, want feature", loaded.Branch)
	}
	if len(loaded.OpenTabs) != 1 || loaded.OpenTabs[0].SessionName != "session-a" {
		t.Fatalf("OpenTabs = %#v, want one tab session-a", loaded.OpenTabs)
	}

	// Sanity: the committed file is intact JSON regardless of the stale artifact.
	if _, statErr := os.Stat(committedPath); statErr != nil {
		t.Fatalf("committed workspace.json missing: %v", statErr)
	}
	_ = filepath.Base(stalePath)
}

func TestWorkspaceStoreAppendOpenTabConcurrentWriters(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	ws := NewWorkspace("ws-a", "main", "origin/main", "/repo", "/repo/ws-a")
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	tabs := []TabInfo{
		{Assistant: "claude", Name: "claude", SessionName: "session-a", Status: "running", CreatedAt: time.Now().Unix()},
		{Assistant: "codex", Name: "codex", SessionName: "session-b", Status: "running", CreatedAt: time.Now().Unix()},
	}

	var wg sync.WaitGroup
	wg.Add(len(tabs))
	for i := range tabs {
		tab := tabs[i]
		go func() {
			defer wg.Done()
			if err := store.AppendOpenTab(ws.ID(), tab); err != nil {
				t.Errorf("AppendOpenTab(%s) error = %v", tab.SessionName, err)
			}
		}()
	}
	wg.Wait()

	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.OpenTabs) != 2 {
		t.Fatalf("open tabs = %d, want 2", len(loaded.OpenTabs))
	}
}
