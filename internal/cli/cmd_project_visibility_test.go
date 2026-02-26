package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestCmdProjectList_DefaultHidesNonGitRegisteredPaths(t *testing.T) {
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

	stalePath := filepath.Join(t.TempDir(), "stale-repo")

	registry := data.NewRegistry(filepath.Join(home, ".amux", "projects.json"))
	if err := registry.AddProject(validRepo); err != nil {
		t.Fatalf("registry.AddProject(validRepo) error = %v", err)
	}
	if err := registry.AddProject(stalePath); err != nil {
		t.Fatalf("registry.AddProject(stalePath) error = %v", err)
	}

	var out, errOut bytes.Buffer
	code := cmdProjectList(&out, &errOut, GlobalFlags{JSON: true}, nil, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdProjectList() code = %d; stderr: %s", code, errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true; raw=%s", out.String())
	}

	entries, ok := env.Data.([]any)
	if !ok {
		t.Fatalf("expected data to be array, got %T", env.Data)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 visible project, got %d", len(entries))
	}
	entry, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatalf("expected entry to be object, got %T", entries[0])
	}
	if got, _ := entry["path"].(string); got != lenientCanonicalizePath(validRepo) {
		t.Fatalf("path = %q, want %q", got, lenientCanonicalizePath(validRepo))
	}
}

func TestCmdProjectList_AllIncludesNonGitRegisteredPaths(t *testing.T) {
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

	stalePath := filepath.Join(t.TempDir(), "stale-repo")

	registry := data.NewRegistry(filepath.Join(home, ".amux", "projects.json"))
	if err := registry.AddProject(validRepo); err != nil {
		t.Fatalf("registry.AddProject(validRepo) error = %v", err)
	}
	if err := registry.AddProject(stalePath); err != nil {
		t.Fatalf("registry.AddProject(stalePath) error = %v", err)
	}

	var out, errOut bytes.Buffer
	code := cmdProjectList(&out, &errOut, GlobalFlags{JSON: true}, []string{"--all"}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdProjectList(--all) code = %d; stderr: %s", code, errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true; raw=%s", out.String())
	}

	entries, ok := env.Data.([]any)
	if !ok {
		t.Fatalf("expected data to be array, got %T", env.Data)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 projects with --all, got %d", len(entries))
	}
	seen := map[string]bool{}
	for _, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("expected entry to be object, got %T", raw)
		}
		path, _ := entry["path"].(string)
		seen[path] = true
	}
	if !seen[lenientCanonicalizePath(validRepo)] || !seen[lenientCanonicalizePath(stalePath)] {
		t.Fatalf("expected both valid and stale paths, got %#v", seen)
	}
}

func TestCmdProjectList_DefaultDedupesCanonicalAliasEntries(t *testing.T) {
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

	var out, errOut bytes.Buffer
	code := cmdProjectList(&out, &errOut, GlobalFlags{JSON: true}, nil, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdProjectList() code = %d; stderr: %s", code, errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true; raw=%s", out.String())
	}

	entries, ok := env.Data.([]any)
	if !ok {
		t.Fatalf("expected data to be array, got %T", env.Data)
	}
	if len(entries) != 1 {
		t.Fatalf("expected canonical alias entries to collapse to 1, got %d", len(entries))
	}
}
