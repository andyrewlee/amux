package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestCmdWorkspaceList_DefaultHidesUnmanagedWorkspaceMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRegistered, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}

	registryPath := filepath.Join(home, ".amux", "projects.json")
	if err := data.NewRegistry(registryPath).AddProject(repoRegistered); err != nil {
		t.Fatalf("AddProject() error = %v", err)
	}

	managedRoot := filepath.Join(home, ".amux", "workspaces", filepath.Base(repoRegistered), "feature-managed")
	unmanagedRoot := filepath.Join(t.TempDir(), "feature-unmanaged")

	store := data.NewWorkspaceStore(filepath.Join(home, ".amux", "workspaces-metadata"))
	if err := store.Save(&data.Workspace{
		Name:   "feature-managed",
		Branch: "feature-managed",
		Repo:   repoRegistered,
		Root:   managedRoot,
	}); err != nil {
		t.Fatalf("Save(managed) error = %v", err)
	}
	if err := store.Save(&data.Workspace{
		Name:   "feature-unmanaged",
		Branch: "feature-unmanaged",
		Repo:   repoRegistered,
		Root:   unmanagedRoot,
	}); err != nil {
		t.Fatalf("Save(unmanaged) error = %v", err)
	}

	var w, wErr bytes.Buffer
	code := cmdWorkspaceList(&w, &wErr, GlobalFlags{JSON: true}, nil, "test-v1")
	if code != ExitOK {
		t.Fatalf("expected ExitOK, got %d; stderr: %s", code, wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, w.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true")
	}

	rows, ok := env.Data.([]any)
	if !ok {
		t.Fatalf("expected []any data, got %T", env.Data)
	}
	foundManaged := false
	foundUnmanaged := false
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		root, _ := m["root"].(string)
		if root == managedRoot {
			foundManaged = true
		}
		if root == unmanagedRoot {
			foundUnmanaged = true
		}
	}
	if !foundManaged {
		t.Fatalf("expected managed workspace root %q in results", managedRoot)
	}
	if foundUnmanaged {
		t.Fatalf("expected unmanaged workspace root %q to be hidden", unmanagedRoot)
	}
}

func TestCmdWorkspaceList_ByRepoHidesUnmanagedWorkspaceMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRegistered, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}

	registryPath := filepath.Join(home, ".amux", "projects.json")
	if err := data.NewRegistry(registryPath).AddProject(repoRegistered); err != nil {
		t.Fatalf("AddProject() error = %v", err)
	}

	managedRoot := filepath.Join(home, ".amux", "workspaces", filepath.Base(repoRegistered), "feature-managed")
	unmanagedRoot := filepath.Join(t.TempDir(), "feature-unmanaged")

	store := data.NewWorkspaceStore(filepath.Join(home, ".amux", "workspaces-metadata"))
	if err := store.Save(&data.Workspace{
		Name:   "feature-managed",
		Branch: "feature-managed",
		Repo:   repoRegistered,
		Root:   managedRoot,
	}); err != nil {
		t.Fatalf("Save(managed) error = %v", err)
	}
	if err := store.Save(&data.Workspace{
		Name:   "feature-unmanaged",
		Branch: "feature-unmanaged",
		Repo:   repoRegistered,
		Root:   unmanagedRoot,
	}); err != nil {
		t.Fatalf("Save(unmanaged) error = %v", err)
	}

	var w, wErr bytes.Buffer
	code := cmdWorkspaceList(&w, &wErr, GlobalFlags{JSON: true}, []string{"--repo", repoRegistered}, "test-v1")
	if code != ExitOK {
		t.Fatalf("expected ExitOK, got %d; stderr: %s", code, wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, w.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true")
	}

	rows, ok := env.Data.([]any)
	if !ok {
		t.Fatalf("expected []any data, got %T", env.Data)
	}
	foundManaged := false
	foundUnmanaged := false
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		root, _ := m["root"].(string)
		if root == managedRoot {
			foundManaged = true
		}
		if root == unmanagedRoot {
			foundUnmanaged = true
		}
	}
	if !foundManaged {
		t.Fatalf("expected managed workspace root %q in results", managedRoot)
	}
	if foundUnmanaged {
		t.Fatalf("expected unmanaged workspace root %q to be hidden", unmanagedRoot)
	}
}
