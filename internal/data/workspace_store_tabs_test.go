package data

import (
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
