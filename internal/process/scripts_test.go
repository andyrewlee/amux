package process

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

func writeProjectConfig(t *testing.T, repoPath, content string) {
	configDir := filepath.Join(repoPath, ".amux")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir .amux: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "project.json"), []byte(content), 0644); err != nil {
		t.Fatalf("write project.json: %v", err)
	}
}

func TestScriptRunnerLoadConfigMissing(t *testing.T) {
	repo := t.TempDir()
	runner := NewScriptRunner(6200, 10)

	cfg, err := runner.LoadConfig(repo)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.RunScript != "" || cfg.ArchiveScript != "" || len(cfg.SetupScripts) != 0 {
		t.Fatalf("expected empty config when file missing, got %+v", cfg)
	}
}

func TestScriptRunnerLoadConfigMalformedJSON(t *testing.T) {
	repo := t.TempDir()
	writeProjectConfig(t, repo, `{invalid json}`)

	runner := NewScriptRunner(6200, 10)
	_, err := runner.LoadConfig(repo)
	if err == nil {
		t.Fatalf("LoadConfig() should fail for malformed JSON")
	}
}

func TestScriptRunnerLoadConfigValidJSON(t *testing.T) {
	repo := t.TempDir()
	writeProjectConfig(t, repo, `{
  "setupScripts": ["echo setup1", "echo setup2"],
  "runScript": "npm start",
  "archiveScript": "tar -czf archive.tar.gz ."
}`)

	runner := NewScriptRunner(6200, 10)
	cfg, err := runner.LoadConfig(repo)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.SetupScripts) != 2 {
		t.Fatalf("expected 2 setup commands, got %d", len(cfg.SetupScripts))
	}
	if cfg.RunScript != "npm start" {
		t.Fatalf("expected run script 'npm start', got %s", cfg.RunScript)
	}
	if cfg.ArchiveScript != "tar -czf archive.tar.gz ." {
		t.Fatalf("expected archive script, got %s", cfg.ArchiveScript)
	}
}

func TestScriptRunnerRunSetupAndEnv(t *testing.T) {
	repo := t.TempDir()
	workspaceRoot := t.TempDir()

	writeProjectConfig(t, repo, `{
  "setupScripts": ["printf \"$AMUX_WORKSPACE_NAME-$CUSTOM_VAR\" > setup.txt"]
}`)

	runner := NewScriptRunner(6200, 10)
	ws := &data.Workspace{
		Name:   "feature-1",
		Branch: "feature-1",
		Repo:   repo,
		Root:   workspaceRoot,
		Env:    map[string]string{"CUSTOM_VAR": "hello"},
	}

	if err := runner.RunSetup(ws); err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(workspaceRoot, "setup.txt"))
	if err != nil {
		t.Fatalf("expected setup.txt to exist: %v", err)
	}
	if strings.TrimSpace(string(contents)) != "feature-1-hello" {
		t.Fatalf("unexpected setup.txt contents: %s", contents)
	}
}

func TestScriptRunnerRunSetupFailure(t *testing.T) {
	repo := t.TempDir()
	workspaceRoot := t.TempDir()

	writeProjectConfig(t, repo, `{
  "setupScripts": ["exit 1"]
}`)

	runner := NewScriptRunner(6200, 10)
	ws := &data.Workspace{Repo: repo, Root: workspaceRoot}

	if err := runner.RunSetup(ws); err == nil {
		t.Fatalf("expected RunSetup() to fail for failing command")
	}
}

func TestScriptRunnerRunScriptConfigAndWorkspaceScripts(t *testing.T) {
	repo := t.TempDir()
	workspaceRoot := t.TempDir()

	writeProjectConfig(t, repo, `{
  "runScript": "printf run-config > run.txt"
}`)

	runner := NewScriptRunner(6200, 10)
	ws := &data.Workspace{Repo: repo, Root: workspaceRoot}

	_, err := runner.RunScript(ws, ScriptRun)
	if err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}
	if err := waitForFile(filepath.Join(workspaceRoot, "run.txt"), 2*time.Second); err != nil {
		t.Fatalf("expected run.txt to be created: %v", err)
	}

	// Now test workspace scripts fallback when config missing.
	writeProjectConfig(t, repo, `{}`)
	ws.Scripts = data.ScriptsConfig{Run: "printf run-workspace > run-workspace.txt"}
	_, err = runner.RunScript(ws, ScriptRun)
	if err != nil {
		t.Fatalf("RunScript() workspace scripts error = %v", err)
	}
	if err := waitForFile(filepath.Join(workspaceRoot, "run-workspace.txt"), 2*time.Second); err != nil {
		t.Fatalf("expected run-workspace.txt to be created: %v", err)
	}
}

func TestScriptRunnerRunScriptMissing(t *testing.T) {
	repo := t.TempDir()
	workspaceRoot := t.TempDir()

	writeProjectConfig(t, repo, `{}`)

	runner := NewScriptRunner(6200, 10)
	ws := &data.Workspace{Repo: repo, Root: workspaceRoot}

	if _, err := runner.RunScript(ws, ScriptRun); err == nil {
		t.Fatalf("expected RunScript() to fail when no script configured")
	}
}

func TestScriptRunnerStop(t *testing.T) {
	repo := t.TempDir()
	workspaceRoot := t.TempDir()

	writeProjectConfig(t, repo, `{
  "runScript": "sleep 5"
}`)

	runner := NewScriptRunner(6200, 10)
	ws := &data.Workspace{Repo: repo, Root: workspaceRoot}

	if _, err := runner.RunScript(ws, ScriptRun); err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}

	if !runner.IsRunning(ws) {
		t.Fatalf("expected script to be running")
	}

	if err := runner.Stop(ws); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	deadline := time.After(2 * time.Second)
	for runner.IsRunning(ws) {
		select {
		case <-deadline:
			t.Fatalf("script did not stop in time")
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
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
