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

func TestRouteProjectNoSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := routeProject(&out, &errOut, GlobalFlags{JSON: true}, nil, "test-v1")
	if code != ExitUsage {
		t.Fatalf("routeProject() code = %d, want %d", code, ExitUsage)
	}
	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
}

func TestRouteProjectUnknownSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := routeProject(&out, &errOut, GlobalFlags{JSON: true}, []string{"bogus"}, "test-v1")
	if code != ExitUsage {
		t.Fatalf("routeProject() code = %d, want %d", code, ExitUsage)
	}
	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || !strings.Contains(env.Error.Message, "bogus") {
		t.Fatalf("expected unknown subcommand message mentioning 'bogus', got %#v", env.Error)
	}
}

func TestCmdProjectListEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

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
	if len(entries) != 0 {
		t.Fatalf("expected empty project list, got %d entries", len(entries))
	}
}

func TestCmdProjectListWithProjects(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRoot := filepath.Join(t.TempDir(), "myrepo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	runGit(t, repoRoot, "init")

	// Register via project add
	var addOut, addErr bytes.Buffer
	addCode := cmdProjectAdd(&addOut, &addErr, GlobalFlags{JSON: true}, []string{repoRoot}, "test-v1")
	if addCode != ExitOK {
		t.Fatalf("cmdProjectAdd() code = %d; stderr: %s", addCode, addErr.String())
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
		t.Fatalf("expected 1 project, got %d", len(entries))
	}
	entry, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatalf("expected entry to be object, got %T", entries[0])
	}
	if entry["name"] != "myrepo" {
		t.Fatalf("name = %q, want %q", entry["name"], "myrepo")
	}
}

func TestCmdProjectAddRegistersProject(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRoot := filepath.Join(t.TempDir(), "addme")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	runGit(t, repoRoot, "init")

	var out, errOut bytes.Buffer
	code := cmdProjectAdd(&out, &errOut, GlobalFlags{JSON: true}, []string{repoRoot}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdProjectAdd() code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true; raw=%s", out.String())
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T", env.Data)
	}
	if data["name"] != "addme" {
		t.Fatalf("name = %q, want %q", data["name"], "addme")
	}

	// Verify it appears in list
	var listOut, listErr bytes.Buffer
	listCode := cmdProjectList(&listOut, &listErr, GlobalFlags{JSON: true}, nil, "test-v1")
	if listCode != ExitOK {
		t.Fatalf("cmdProjectList() code = %d", listCode)
	}
	var listEnv Envelope
	if err := json.Unmarshal(listOut.Bytes(), &listEnv); err != nil {
		t.Fatalf("json.Unmarshal(list) error = %v", err)
	}
	entries, _ := listEnv.Data.([]any)
	if len(entries) != 1 {
		t.Fatalf("expected 1 project in list, got %d", len(entries))
	}
}

func TestCmdProjectAddNotGitRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	notRepo := t.TempDir()

	var out, errOut bytes.Buffer
	code := cmdProjectAdd(&out, &errOut, GlobalFlags{JSON: true}, []string{notRepo}, "test-v1")
	if code != ExitUsage {
		t.Fatalf("cmdProjectAdd() code = %d, want %d", code, ExitUsage)
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "not_git_repo" {
		t.Fatalf("expected not_git_repo error, got %#v", env.Error)
	}
}

func TestCmdProjectAddDuplicate(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRoot := filepath.Join(t.TempDir(), "duprepo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	runGit(t, repoRoot, "init")

	// Add twice — should be idempotent.
	for i := 0; i < 2; i++ {
		var out, errOut bytes.Buffer
		code := cmdProjectAdd(&out, &errOut, GlobalFlags{JSON: true}, []string{repoRoot}, "test-v1")
		if code != ExitOK {
			t.Fatalf("cmdProjectAdd() attempt %d code = %d; stderr: %s", i+1, code, errOut.String())
		}
	}

	// Verify only one entry in list
	var listOut, listErr bytes.Buffer
	listCode := cmdProjectList(&listOut, &listErr, GlobalFlags{JSON: true}, nil, "test-v1")
	if listCode != ExitOK {
		t.Fatalf("cmdProjectList() code = %d", listCode)
	}
	var env Envelope
	if err := json.Unmarshal(listOut.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal(list) error = %v", err)
	}
	entries, _ := env.Data.([]any)
	if len(entries) != 1 {
		t.Fatalf("expected 1 project after duplicate add, got %d", len(entries))
	}
}

func TestCmdProjectRemoveSuccess(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRoot := filepath.Join(t.TempDir(), "removeme")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	runGit(t, repoRoot, "init")

	// Add first
	var addOut, addErr bytes.Buffer
	addCode := cmdProjectAdd(&addOut, &addErr, GlobalFlags{JSON: true}, []string{repoRoot}, "test-v1")
	if addCode != ExitOK {
		t.Fatalf("cmdProjectAdd() code = %d", addCode)
	}

	// Remove
	var out, errOut bytes.Buffer
	code := cmdProjectRemove(&out, &errOut, GlobalFlags{JSON: true}, []string{repoRoot}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdProjectRemove() code = %d; stderr: %s", code, errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true; raw=%s", out.String())
	}

	// Verify list is empty
	var listOut, listErr bytes.Buffer
	listCode := cmdProjectList(&listOut, &listErr, GlobalFlags{JSON: true}, nil, "test-v1")
	if listCode != ExitOK {
		t.Fatalf("cmdProjectList() code = %d", listCode)
	}
	var listEnv Envelope
	if err := json.Unmarshal(listOut.Bytes(), &listEnv); err != nil {
		t.Fatalf("json.Unmarshal(list) error = %v", err)
	}
	entries, _ := listEnv.Data.([]any)
	if len(entries) != 0 {
		t.Fatalf("expected 0 projects after remove, got %d", len(entries))
	}
}

func TestCmdProjectRemoveNotRegistered(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	notRegistered := t.TempDir()

	var out, errOut bytes.Buffer
	code := cmdProjectRemove(&out, &errOut, GlobalFlags{JSON: true}, []string{notRegistered}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdProjectRemove() code = %d, want %d; stderr: %s", code, ExitOK, errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true (remove is idempotent); raw=%s", out.String())
	}
}

func TestCmdProjectRemoveMatchesStoredPathWithoutSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink path canonicalization path is unstable on windows in test environment")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	realRepo := filepath.Join(t.TempDir(), "real-repo")
	if err := os.MkdirAll(realRepo, 0o755); err != nil {
		t.Fatalf("MkdirAll(realRepo) error = %v", err)
	}
	storedLink := filepath.Join(t.TempDir(), "linked-repo")
	if err := os.Symlink(realRepo, storedLink); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	registry := data.NewRegistry(filepath.Join(home, ".amux", "projects.json"))
	if err := registry.AddProject(storedLink); err != nil {
		t.Fatalf("registry.AddProject(%q) error = %v", storedLink, err)
	}

	// Remove using the symlink path so lenient canonicalization resolves it away,
	// while no-resolve canonicalization keeps the stored entry matchable.
	var out, errOut bytes.Buffer
	code := cmdProjectRemove(&out, &errOut, GlobalFlags{JSON: true}, []string{storedLink}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdProjectRemove() code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true; raw=%s", out.String())
	}

	// Verify stale non-resolved entry is gone.
	var listOut, listErr bytes.Buffer
	listCode := cmdProjectList(&listOut, &listErr, GlobalFlags{JSON: true}, nil, "test-v1")
	if listCode != ExitOK {
		t.Fatalf("cmdProjectList() code = %d", listCode)
	}
	var listEnv Envelope
	if err := json.Unmarshal(listOut.Bytes(), &listEnv); err != nil {
		t.Fatalf("json.Unmarshal(list) error = %v", err)
	}
	entries, _ := listEnv.Data.([]any)
	if len(entries) != 0 {
		t.Fatalf("expected 0 projects after remove, got %d", len(entries))
	}
}

func TestCmdProjectRemoveDeletedPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRoot := filepath.Join(t.TempDir(), "ephemeral")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	runGit(t, repoRoot, "init")

	// Register while it still exists.
	var addOut, addErr bytes.Buffer
	addCode := cmdProjectAdd(&addOut, &addErr, GlobalFlags{JSON: true}, []string{repoRoot}, "test-v1")
	if addCode != ExitOK {
		t.Fatalf("cmdProjectAdd() code = %d", addCode)
	}

	// Delete the directory.
	if err := os.RemoveAll(repoRoot); err != nil {
		t.Fatalf("RemoveAll() error = %v", err)
	}

	// Remove should still succeed even though the path no longer exists.
	var out, errOut bytes.Buffer
	code := cmdProjectRemove(&out, &errOut, GlobalFlags{JSON: true}, []string{repoRoot}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdProjectRemove() code = %d, want %d; stderr: %s; stdout: %s", code, ExitOK, errOut.String(), out.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true; raw=%s", out.String())
	}

	// Verify list is now empty.
	var listOut, listErr bytes.Buffer
	listCode := cmdProjectList(&listOut, &listErr, GlobalFlags{JSON: true}, nil, "test-v1")
	if listCode != ExitOK {
		t.Fatalf("cmdProjectList() code = %d", listCode)
	}
	var listEnv Envelope
	if err := json.Unmarshal(listOut.Bytes(), &listEnv); err != nil {
		t.Fatalf("json.Unmarshal(list) error = %v", err)
	}
	entries, _ := listEnv.Data.([]any)
	if len(entries) != 0 {
		t.Fatalf("expected 0 projects after removing deleted path, got %d", len(entries))
	}
}
