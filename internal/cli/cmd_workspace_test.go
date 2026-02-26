package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestCmdWorkspaceListByRelativeRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer func() { _ = os.Chdir(originalWD) }()

	// Use the current repo directory so "." resolves to a real path.
	if err := os.Chdir(originalWD); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	var w, wErr bytes.Buffer
	gf := GlobalFlags{JSON: true}
	code := cmdWorkspaceList(&w, &wErr, gf, []string{"--repo", "."}, "test-v1")
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
}

func TestCmdWorkspaceListByRelativeProjectAlias(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer func() { _ = os.Chdir(originalWD) }()

	if err := os.Chdir(originalWD); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	var w, wErr bytes.Buffer
	gf := GlobalFlags{JSON: true}
	code := cmdWorkspaceList(&w, &wErr, gf, []string{"--project", "."}, "test-v1")
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
}

func TestCmdWorkspaceListRejectsRepoAndProjectTogether(t *testing.T) {
	var w, wErr bytes.Buffer
	gf := GlobalFlags{JSON: true}
	code := cmdWorkspaceList(
		&w,
		&wErr,
		gf,
		[]string{"--repo", "/tmp/repo-a", "--project", "/tmp/repo-b"},
		"test-v1",
	)
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage, got %d; stderr: %s", code, wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, w.String())
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
}

func TestCmdWorkspaceListRejectsAllWithRepoFilter(t *testing.T) {
	var w, wErr bytes.Buffer
	gf := GlobalFlags{JSON: true}
	code := cmdWorkspaceList(
		&w,
		&wErr,
		gf,
		[]string{"--all", "--repo", "/tmp/repo-a"},
		"test-v1",
	)
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage, got %d; stderr: %s", code, wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, w.String())
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
}

func TestCmdWorkspaceListJSON(t *testing.T) {
	var w, wErr bytes.Buffer
	gf := GlobalFlags{JSON: true}
	code := cmdWorkspaceList(&w, &wErr, gf, nil, "test-v1")

	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode JSON: %v\nraw: %s", err, w.String())
	}
	if !env.OK {
		t.Error("expected ok=true")
	}

	// Data should be an array (possibly empty)
	if env.Data == nil {
		t.Fatal("expected data to be set")
	}
}

func TestCmdWorkspaceListHuman(t *testing.T) {
	var w, wErr bytes.Buffer
	gf := GlobalFlags{JSON: false}
	code := cmdWorkspaceList(&w, &wErr, gf, nil, "test-v1")

	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, wErr.String())
	}
}

func TestCmdWorkspaceListJSONReturnsInternalErrorOnCorruptMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wsID := "0123456789abcdef"
	metaDir := filepath.Join(home, ".amux", "workspaces-metadata", wsID)
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, "workspace.json"), []byte("{invalid"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var w, wErr bytes.Buffer
	code := cmdWorkspaceList(&w, &wErr, GlobalFlags{JSON: true}, []string{"--all"}, "test-v1")
	if code != ExitInternalError {
		t.Fatalf("expected exit %d, got %d", ExitInternalError, code)
	}
	if wErr.Len() != 0 {
		t.Fatalf("expected empty stderr in JSON mode, got %q", wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode JSON: %v\nraw: %s", err, w.String())
	}
	if env.OK {
		t.Fatal("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "list_failed" {
		t.Fatalf("expected list_failed error, got %#v", env.Error)
	}
}

func TestCmdWorkspaceList_DefaultHidesUnregisteredWorkspaceMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRegistered, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	repoUnregistered := t.TempDir()

	registryPath := filepath.Join(home, ".amux", "projects.json")
	if err := data.NewRegistry(registryPath).AddProject(repoRegistered); err != nil {
		t.Fatalf("AddProject() error = %v", err)
	}

	store := data.NewWorkspaceStore(filepath.Join(home, ".amux", "workspaces-metadata"))
	if err := store.Save(&data.Workspace{
		Name:   "registered-main",
		Branch: "main",
		Repo:   repoRegistered,
		Root:   repoRegistered,
	}); err != nil {
		t.Fatalf("Save(registered) error = %v", err)
	}
	if err := store.Save(&data.Workspace{
		Name:   "stale-main",
		Branch: "main",
		Repo:   repoUnregistered,
		Root:   repoUnregistered,
	}); err != nil {
		t.Fatalf("Save(unregistered) error = %v", err)
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
	foundRegistered := false
	foundUnregistered := false
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		repo, _ := m["repo"].(string)
		if repo == repoRegistered {
			foundRegistered = true
		}
		if repo == repoUnregistered {
			foundUnregistered = true
		}
	}
	if !foundRegistered {
		t.Fatalf("expected registered workspace repo %q in results", repoRegistered)
	}
	if foundUnregistered {
		t.Fatalf("expected unregistered workspace repo %q to be hidden", repoUnregistered)
	}
}

func TestCmdWorkspaceList_AllIncludesUnregisteredWorkspaceMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRegistered, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	repoUnregistered := t.TempDir()

	registryPath := filepath.Join(home, ".amux", "projects.json")
	if err := data.NewRegistry(registryPath).AddProject(repoRegistered); err != nil {
		t.Fatalf("AddProject() error = %v", err)
	}

	store := data.NewWorkspaceStore(filepath.Join(home, ".amux", "workspaces-metadata"))
	if err := store.Save(&data.Workspace{
		Name:   "registered-main",
		Branch: "main",
		Repo:   repoRegistered,
		Root:   repoRegistered,
	}); err != nil {
		t.Fatalf("Save(registered) error = %v", err)
	}
	if err := store.Save(&data.Workspace{
		Name:   "stale-main",
		Branch: "main",
		Repo:   repoUnregistered,
		Root:   repoUnregistered,
	}); err != nil {
		t.Fatalf("Save(unregistered) error = %v", err)
	}

	var w, wErr bytes.Buffer
	code := cmdWorkspaceList(&w, &wErr, GlobalFlags{JSON: true}, []string{"--all"}, "test-v1")
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
	foundRegistered := false
	foundUnregistered := false
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		repo, _ := m["repo"].(string)
		if repo == repoRegistered {
			foundRegistered = true
		}
		if repo == repoUnregistered {
			foundUnregistered = true
		}
	}
	if !foundRegistered || !foundUnregistered {
		t.Fatalf("expected both repos in --all output (registered=%v, unregistered=%v)", foundRegistered, foundUnregistered)
	}
}

func TestCmdWorkspaceList_ByRepoHidesUnregisteredProjectMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRegistered, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	repoUnregistered := t.TempDir()

	registryPath := filepath.Join(home, ".amux", "projects.json")
	if err := data.NewRegistry(registryPath).AddProject(repoRegistered); err != nil {
		t.Fatalf("AddProject() error = %v", err)
	}

	store := data.NewWorkspaceStore(filepath.Join(home, ".amux", "workspaces-metadata"))
	if err := store.Save(&data.Workspace{
		Name:   "registered-main",
		Branch: "main",
		Repo:   repoRegistered,
		Root:   repoRegistered,
	}); err != nil {
		t.Fatalf("Save(registered) error = %v", err)
	}
	if err := store.Save(&data.Workspace{
		Name:   "stale-main",
		Branch: "main",
		Repo:   repoUnregistered,
		Root:   repoUnregistered,
	}); err != nil {
		t.Fatalf("Save(unregistered) error = %v", err)
	}

	var w, wErr bytes.Buffer
	code := cmdWorkspaceList(
		&w,
		&wErr,
		GlobalFlags{JSON: true},
		[]string{"--repo", repoUnregistered},
		"test-v1",
	)
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
	if len(rows) != 0 {
		t.Fatalf("expected no rows for unregistered repo filter, got %d", len(rows))
	}
}

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
