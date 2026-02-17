package process

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

func writeWorkspaceConfig(t *testing.T, repoPath, content string) {
	configDir := filepath.Join(repoPath, ".amux")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir .amux: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "workspaces.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write workspaces.json: %v", err)
	}
}

func TestScriptRunnerLoadConfigMissing(t *testing.T) {
	repo := t.TempDir()
	runner := NewScriptRunner(6200, 10)

	cfg, err := runner.LoadConfig(repo)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.RunScript != "" || cfg.ArchiveScript != "" || len(cfg.SetupWorkspace) != 0 {
		t.Fatalf("expected empty config when file missing, got %+v", cfg)
	}
}

func TestScriptRunnerLoadConfigMalformedJSON(t *testing.T) {
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{invalid json}`)

	runner := NewScriptRunner(6200, 10)
	_, err := runner.LoadConfig(repo)
	if err == nil {
		t.Fatalf("LoadConfig() should fail for malformed JSON")
	}
}

func TestScriptRunnerLoadConfigValidJSON(t *testing.T) {
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{
  "setup-workspace": ["echo setup1", "echo setup2"],
  "run": "npm start",
  "archive": "tar -czf archive.tar.gz ."
}`)

	runner := NewScriptRunner(6200, 10)
	cfg, err := runner.LoadConfig(repo)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.SetupWorkspace) != 2 {
		t.Fatalf("expected 2 setup commands, got %d", len(cfg.SetupWorkspace))
	}
	if cfg.RunScript != "npm start" {
		t.Fatalf("expected run script 'npm start', got %s", cfg.RunScript)
	}
	if cfg.ArchiveScript != "tar -czf archive.tar.gz ." {
		t.Fatalf("expected archive script, got %s", cfg.ArchiveScript)
	}
}

func TestScriptRunnerLoadConfigPermissionError(t *testing.T) {
	repo := t.TempDir()
	configDir := filepath.Join(repo, ".amux")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir .amux: %v", err)
	}
	configPath := filepath.Join(configDir, "workspaces.json")
	if err := os.WriteFile(configPath, []byte(`{"run":"test"}`), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	// Make file unreadable
	if err := os.Chmod(configPath, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(configPath, 0o644)
	})

	runner := NewScriptRunner(6200, 10)
	_, err := runner.LoadConfig(repo)
	if err == nil {
		t.Fatal("expected permission error, got nil")
	}
	if os.IsNotExist(err) {
		t.Fatalf("expected permission error, got IsNotExist: %v", err)
	}
}

func TestScriptRunnerRunSetupAndEnv(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()

	writeWorkspaceConfig(t, repo, `{
  "setup-workspace": ["printf \"$AMUX_WORKSPACE_NAME-$CUSTOM_VAR\" > setup.txt"]
}`)

	runner := NewScriptRunner(6200, 10)
	wt := &data.Workspace{
		Name:   "feature-1",
		Branch: "feature-1",
		Repo:   repo,
		Root:   wsRoot,
		Env:    map[string]string{"CUSTOM_VAR": "hello"},
	}

	if err := runner.RunSetup(wt); err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(wsRoot, "setup.txt"))
	if err != nil {
		t.Fatalf("expected setup.txt to exist: %v", err)
	}
	if strings.TrimSpace(string(contents)) != "feature-1-hello" {
		t.Fatalf("unexpected setup.txt contents: %s", contents)
	}
}

func TestScriptRunnerRunSetupFailure(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()

	writeWorkspaceConfig(t, repo, `{
  "setup-workspace": ["exit 1"]
}`)

	runner := NewScriptRunner(6200, 10)
	wt := &data.Workspace{Repo: repo, Root: wsRoot}

	if err := runner.RunSetup(wt); err == nil {
		t.Fatalf("expected RunSetup() to fail for failing command")
	}
}

func TestScriptRunnerRunScriptConfigAndWorkspaceScripts(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()

	writeWorkspaceConfig(t, repo, `{
  "run": "printf run-config > run.txt"
}`)

	runner := NewScriptRunner(6200, 10)
	wt := &data.Workspace{Repo: repo, Root: wsRoot}

	_, err := runner.RunScript(wt, ScriptRun)
	if err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}
	if err := waitForFile(filepath.Join(wsRoot, "run.txt"), 2*time.Second); err != nil {
		t.Fatalf("expected run.txt to be created: %v", err)
	}

	// Now test workspace scripts fallback when config missing.
	writeWorkspaceConfig(t, repo, `{}`)
	wt.Scripts = data.ScriptsConfig{Run: "printf run-workspace > run-workspace.txt"}
	_, err = runner.RunScript(wt, ScriptRun)
	if err != nil {
		t.Fatalf("RunScript() workspace scripts error = %v", err)
	}
	if err := waitForFile(filepath.Join(wsRoot, "run-workspace.txt"), 2*time.Second); err != nil {
		t.Fatalf("expected run-workspace.txt to be created: %v", err)
	}
}

func TestScriptRunnerRunScriptMissing(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()

	writeWorkspaceConfig(t, repo, `{}`)

	runner := NewScriptRunner(6200, 10)
	wt := &data.Workspace{Repo: repo, Root: wsRoot}

	if _, err := runner.RunScript(wt, ScriptRun); err == nil {
		t.Fatalf("expected RunScript() to fail when no script configured")
	}
}

func TestScriptRunnerStop(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()

	writeWorkspaceConfig(t, repo, `{
  "run": "sleep 5"
}`)

	runner := NewScriptRunner(6200, 10)
	wt := &data.Workspace{Repo: repo, Root: wsRoot}

	if _, err := runner.RunScript(wt, ScriptRun); err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}

	if !runner.IsRunning(wt) {
		t.Fatalf("expected script to be running")
	}

	if err := runner.Stop(wt); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	deadline := time.After(2 * time.Second)
	for runner.IsRunning(wt) {
		select {
		case <-deadline:
			t.Fatalf("script did not stop in time")
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func TestScriptRunnerWorkspaceValidation(t *testing.T) {
	runner := NewScriptRunner(6200, 10)

	// nil workspace
	if err := runner.RunSetup(nil); err == nil {
		t.Fatal("expected error for nil workspace")
	}
	if _, err := runner.RunScript(nil, ScriptRun); err == nil {
		t.Fatal("expected error for nil workspace")
	}
	if err := runner.Stop(nil); err == nil {
		t.Fatal("expected error for nil workspace")
	}
	if runner.IsRunning(nil) {
		t.Fatal("expected false for nil workspace")
	}

	// empty repo
	ws := &data.Workspace{Repo: "", Root: "/some/root"}
	if err := runner.RunSetup(ws); err == nil {
		t.Fatal("expected error for empty repo")
	}

	// empty root
	ws = &data.Workspace{Repo: "/some/repo", Root: ""}
	if err := runner.RunSetup(ws); err == nil {
		t.Fatal("expected error for empty root")
	}
}

func TestScriptRunnerUsesNormalizedWorkspaceKey(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()

	writeWorkspaceConfig(t, repo, `{"run": "sleep 5"}`)

	runner := NewScriptRunner(6200, 10)
	ws1 := &data.Workspace{Repo: repo, Root: wsRoot}

	if _, err := runner.RunScript(ws1, ScriptRun); err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}

	// Check via a workspace with an equivalent but non-identical path
	// (trailing slash variation, which filepath.Clean normalizes)
	ws2 := &data.Workspace{Repo: repo, Root: wsRoot + "/"}
	if !runner.IsRunning(ws2) {
		t.Fatal("expected script to be running via normalized key")
	}

	// Clean up
	_ = runner.Stop(ws1)
}

func TestScriptRunnerRunScriptNonconcurrentStopFailure(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()

	writeWorkspaceConfig(t, repo, `{"run": "sleep 5"}`)

	runner := NewScriptRunner(6200, 10)
	ws := &data.Workspace{Repo: repo, Root: wsRoot, ScriptMode: "nonconcurrent"}

	// Start a script first
	if _, err := runner.RunScript(ws, ScriptRun); err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Inject a non-benign stop error via the struct field
	origKill := runner.killProcessGroup
	runner.killProcessGroup = func(pid int, opts KillOptions) error {
		return errors.New("permission denied")
	}

	// Second run in nonconcurrent mode should fail because stop fails
	_, err := runner.RunScript(ws, ScriptRun)
	if err == nil {
		t.Fatal("expected error from non-benign stop failure")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected permission denied error, got: %v", err)
	}

	// Clean up with original kill
	runner.killProcessGroup = origKill
	_ = runner.Stop(ws)
}

func TestScriptRunnerRunScriptNonconcurrentIgnoresBenignStopRace(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()

	writeWorkspaceConfig(t, repo, `{"run": "sleep 5"}`)

	runner := NewScriptRunner(6200, 10)
	ws := &data.Workspace{Repo: repo, Root: wsRoot, ScriptMode: "nonconcurrent"}

	// Start a script first
	if _, err := runner.RunScript(ws, ScriptRun); err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Inject a benign "process already finished" error via the struct field
	origKill := runner.killProcessGroup
	runner.killProcessGroup = func(pid int, opts KillOptions) error {
		return errors.New("process already finished")
	}

	// Second run should succeed because benign stop errors are ignored
	cmd, err := runner.RunScript(ws, ScriptRun)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}

	// Clean up
	runner.killProcessGroup = origKill
	_ = runner.Stop(ws)
}

func waitForFile(path string, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		select {
		case <-deadline:
			return os.ErrNotExist
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}
