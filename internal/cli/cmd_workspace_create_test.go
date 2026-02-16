package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

func TestCanonicalizeProjectPathRelative(t *testing.T) {
	base := t.TempDir()
	project := filepath.Join(base, "repo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatalf("Chdir(%q) error = %v", base, err)
	}
	defer func() { _ = os.Chdir(originalWD) }()

	got, err := canonicalizeProjectPath("./repo")
	if err != nil {
		t.Fatalf("canonicalizeProjectPath() error = %v", err)
	}
	want, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}
	if got != want {
		t.Fatalf("canonicalized project path = %q, want %q", got, want)
	}
}

func TestWaitForPath(t *testing.T) {
	existing := filepath.Join(t.TempDir(), "exists")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := waitForPath(existing, 1, time.Millisecond); err != nil {
		t.Fatalf("waitForPath(existing) error = %v", err)
	}

	missing := filepath.Join(t.TempDir(), "missing")
	if err := waitForPath(missing, 2, time.Millisecond); err == nil {
		t.Fatalf("expected waitForPath(missing) to fail")
	}
}

func TestCmdWorkspaceCreateRelativeProjectRemoveFromDifferentDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(repoRoot) error = %v", err)
	}
	runGit(t, repoRoot, "init")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "amux-test")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	runGit(t, repoRoot, "add", "README.md")
	runGit(t, repoRoot, "commit", "-m", "init")

	registerProject(t, home, repoRoot)

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("Chdir(repoRoot) error = %v", err)
	}
	defer func() { _ = os.Chdir(originalWD) }()

	var out, errOut bytes.Buffer
	createCode := cmdWorkspaceCreate(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"rel-ws", "--project", "."},
		"test-v1",
	)
	if createCode != ExitOK {
		t.Fatalf("cmdWorkspaceCreate() code = %d; stderr: %s", createCode, errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal(create output) error = %v", err)
	}
	if !env.OK {
		t.Fatalf("create output expected ok=true; raw=%s", out.String())
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected create data object, got %T", env.Data)
	}
	workspaceID, _ := data["id"].(string)
	if workspaceID == "" {
		t.Fatalf("expected workspace id in create response")
	}
	gotRepo, _ := data["repo"].(string)
	wantRepo, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks(repoRoot) error = %v", err)
	}
	if gotRepo != wantRepo {
		t.Fatalf("stored repo path = %q, want %q", gotRepo, wantRepo)
	}

	if err := os.Chdir(home); err != nil {
		t.Fatalf("Chdir(home) error = %v", err)
	}

	out.Reset()
	errOut.Reset()
	removeCode := cmdWorkspaceRemove(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{workspaceID, "--yes"},
		"test-v1",
	)
	if removeCode != ExitOK {
		t.Fatalf("cmdWorkspaceRemove() code = %d; stderr: %s; stdout: %s", removeCode, errOut.String(), out.String())
	}
}

func TestCmdWorkspaceCreateWithoutBaseFallsBackToHead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(repoRoot) error = %v", err)
	}
	runGit(t, repoRoot, "init")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "amux-test")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	runGit(t, repoRoot, "add", "README.md")
	runGit(t, repoRoot, "commit", "-m", "init")

	currentBranch := runGitOutput(t, repoRoot, "rev-parse", "--abbrev-ref", "HEAD")
	if currentBranch != "trunk" {
		runGit(t, repoRoot, "branch", "-m", "trunk")
	}

	registerProject(t, home, repoRoot)

	var out, errOut bytes.Buffer
	createCode := cmdWorkspaceCreate(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"head-fallback-ws", "--project", repoRoot},
		"test-v1",
	)
	if createCode != ExitOK {
		t.Fatalf("cmdWorkspaceCreate() code = %d; stderr: %s; stdout: %s", createCode, errOut.String(), out.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal(create output) error = %v", err)
	}
	if !env.OK {
		t.Fatalf("create output expected ok=true; raw=%s", out.String())
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected create data object, got %T", env.Data)
	}
	if gotBase, _ := data["base"].(string); gotBase != "HEAD" {
		t.Fatalf("base = %q, want %q", gotBase, "HEAD")
	}
}

func TestCmdWorkspaceCreateRejectsInvalidWorkspaceName(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cmdWorkspaceCreate(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"feature branch", "--project", t.TempDir()},
		"test-v1",
	)
	if code != ExitUsage {
		t.Fatalf("cmdWorkspaceCreate() code = %d, want %d", code, ExitUsage)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
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
	if env.Error == nil || !strings.Contains(env.Error.Message, "invalid workspace name") {
		t.Fatalf("expected invalid workspace name message, got %#v", env.Error)
	}
}

func TestWorkspaceCreateReadinessWaitConfig(t *testing.T) {
	if workspaceCreateReadyAttempts != 100 {
		t.Fatalf("workspaceCreateReadyAttempts = %d, want %d", workspaceCreateReadyAttempts, 100)
	}
	if workspaceCreateReadyDelay != 50*time.Millisecond {
		t.Fatalf("workspaceCreateReadyDelay = %v, want %v", workspaceCreateReadyDelay, 50*time.Millisecond)
	}
}

func TestCmdWorkspaceCreateRejectsUnregisteredProject(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRoot := filepath.Join(t.TempDir(), "unregistered")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(repoRoot) error = %v", err)
	}
	runGit(t, repoRoot, "init")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "amux-test")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	runGit(t, repoRoot, "add", "README.md")
	runGit(t, repoRoot, "commit", "-m", "init")

	// Do NOT register the project — workspace create should fail.
	var out, errOut bytes.Buffer
	code := cmdWorkspaceCreate(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"feat-ws", "--project", repoRoot},
		"test-v1",
	)
	if code != ExitUsage {
		t.Fatalf("cmdWorkspaceCreate() code = %d, want %d; stderr: %s; stdout: %s", code, ExitUsage, errOut.String(), out.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "project_not_registered" {
		t.Fatalf("expected project_not_registered error, got %#v", env.Error)
	}
	if !strings.Contains(env.Error.Message, "amux project add") {
		t.Fatalf("expected error message to mention 'amux project add', got %q", env.Error.Message)
	}
}

func registerProject(t *testing.T, home, repoRoot string) {
	t.Helper()
	registryPath := filepath.Join(home, ".amux", "projects.json")
	reg := data.NewRegistry(registryPath)
	if err := reg.AddProject(repoRoot); err != nil {
		t.Fatalf("AddProject(%q) error = %v", repoRoot, err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
	return strings.TrimSpace(string(output))
}
