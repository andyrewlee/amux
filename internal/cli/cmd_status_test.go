package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestCmdStatusJSON(t *testing.T) {
	var w, wErr bytes.Buffer
	gf := GlobalFlags{JSON: true}
	code := cmdStatus(&w, &wErr, gf, nil, "test-v1")

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

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be an object, got %T", env.Data)
	}
	if _, exists := data["version"]; !exists {
		t.Error("expected 'version' in data")
	}
	if _, exists := data["tmux_available"]; !exists {
		t.Error("expected 'tmux_available' in data")
	}
}

func TestCmdStatusHuman(t *testing.T) {
	var w, wErr bytes.Buffer
	gf := GlobalFlags{JSON: false}
	code := cmdStatus(&w, &wErr, gf, nil, "test-v1")

	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, wErr.String())
	}

	output := w.String()
	if output == "" {
		t.Error("expected non-empty human output")
	}
}

func TestCmdStatusUnexpectedArgsReturnsUsageError(t *testing.T) {
	var w bytes.Buffer
	var wErr bytes.Buffer
	code := cmdStatus(&w, &wErr, GlobalFlags{JSON: true}, []string{"garbage"}, "test-v1")
	if code != ExitUsage {
		t.Fatalf("cmdStatus() code = %d, want %d", code, ExitUsage)
	}
	if wErr.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", wErr.String())
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
	if env.Error == nil || !strings.Contains(env.Error.Message, "unexpected arguments") {
		t.Fatalf("unexpected usage_error message: %#v", env.Error)
	}
}

func TestCmdStatusCountsVisibleProjectsAndStoredWorkspaces(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	validRepo := filepath.Join(t.TempDir(), "valid-repo")
	if err := os.MkdirAll(validRepo, 0o755); err != nil {
		t.Fatalf("MkdirAll(validRepo) error = %v", err)
	}
	runGit(t, validRepo, "init")

	staleRepo := filepath.Join(t.TempDir(), "stale-repo")

	registry := data.NewRegistry(filepath.Join(home, ".amux", "projects.json"))
	if err := registry.AddProject(validRepo); err != nil {
		t.Fatalf("registry.AddProject(validRepo) error = %v", err)
	}
	if err := registry.AddProject(staleRepo); err != nil {
		t.Fatalf("registry.AddProject(staleRepo) error = %v", err)
	}

	store := data.NewWorkspaceStore(filepath.Join(home, ".amux", "workspaces-metadata"))
	validWS := data.NewWorkspace("valid", "main", "", validRepo, filepath.Join(home, ".amux", "workspaces", filepath.Base(validRepo), "valid"))
	if err := store.Save(validWS); err != nil {
		t.Fatalf("store.Save(validWS) error = %v", err)
	}
	staleWS := data.NewWorkspace("stale", "main", "", staleRepo, filepath.Join(staleRepo, ".amux", "workspaces", "stale"))
	if err := store.Save(staleWS); err != nil {
		t.Fatalf("store.Save(staleWS) error = %v", err)
	}

	var w, wErr bytes.Buffer
	code := cmdStatus(&w, &wErr, GlobalFlags{JSON: true}, nil, "test-v1")
	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode JSON: %v\nraw: %s", err, w.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true")
	}

	dataMap, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T", env.Data)
	}

	if got, _ := dataMap["project_count"].(float64); int(got) != 1 {
		t.Fatalf("project_count = %v, want 1", dataMap["project_count"])
	}
	if got, _ := dataMap["workspace_count"].(float64); int(got) != 2 {
		t.Fatalf("workspace_count = %v, want 2", dataMap["workspace_count"])
	}
}

func TestCmdStatusProjectCountDedupesCanonicalAliasEntries(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	if runtime.GOOS == "windows" {
		t.Skip("symlink path canonicalization path is unstable on windows in test environment")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	repoReal := filepath.Join(t.TempDir(), "repo-real")
	if err := os.MkdirAll(repoReal, 0o755); err != nil {
		t.Fatalf("MkdirAll(repoReal) error = %v", err)
	}
	runGit(t, repoReal, "init")
	repoLink := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(repoReal, repoLink); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	registryPath := filepath.Join(home, ".amux", "projects.json")
	if err := os.MkdirAll(filepath.Dir(registryPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(registry dir) error = %v", err)
	}
	raw := `{"projects":[{"name":"repo","path":"` + repoReal + `"},{"name":"repo","path":"` + repoLink + `"}]}`
	if err := os.WriteFile(registryPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile(registry) error = %v", err)
	}

	var w, wErr bytes.Buffer
	code := cmdStatus(&w, &wErr, GlobalFlags{JSON: true}, nil, "test-v1")
	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode JSON: %v\nraw: %s", err, w.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true")
	}

	dataMap, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T", env.Data)
	}
	if got, _ := dataMap["project_count"].(float64); int(got) != 1 {
		t.Fatalf("project_count = %v, want 1", dataMap["project_count"])
	}
}

func TestCmdStatusProjectCountDedupesWhenNormalizeKeyIsEmpty(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	origNormalize := statusNormalizeRepoPathForCompare
	statusNormalizeRepoPathForCompare = func(string) string { return "" }
	defer func() { statusNormalizeRepoPathForCompare = origNormalize }()

	home := t.TempDir()
	t.Setenv("HOME", home)

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(repo) error = %v", err)
	}
	runGit(t, repo, "init")

	registryPath := filepath.Join(home, ".amux", "projects.json")
	if err := os.MkdirAll(filepath.Dir(registryPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(registry dir) error = %v", err)
	}
	raw := `{"projects":[{"name":"repo-1","path":"` + repo + `"},{"name":"repo-2","path":"` + repo + `"}]}`
	if err := os.WriteFile(registryPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile(registry) error = %v", err)
	}

	var w, wErr bytes.Buffer
	code := cmdStatus(&w, &wErr, GlobalFlags{JSON: true}, nil, "test-v1")
	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode JSON: %v\nraw: %s", err, w.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true")
	}

	dataMap, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T", env.Data)
	}
	if got, _ := dataMap["project_count"].(float64); int(got) != 1 {
		t.Fatalf("project_count = %v, want 1", dataMap["project_count"])
	}
}
