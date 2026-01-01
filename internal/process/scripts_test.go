package process

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

func writeWorktreeConfig(t *testing.T, repoPath, content string) {
	configDir := filepath.Join(repoPath, ".amux")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir .amux: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "worktrees.json"), []byte(content), 0644); err != nil {
		t.Fatalf("write worktrees.json: %v", err)
	}
}

func TestScriptRunnerLoadConfigMissing(t *testing.T) {
	repo := t.TempDir()
	runner := NewScriptRunner(6200, 10)

	cfg, err := runner.LoadConfig(repo)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.RunScript != "" || cfg.ArchiveScript != "" || len(cfg.SetupWorktree) != 0 {
		t.Fatalf("expected empty config when file missing, got %+v", cfg)
	}
}

func TestScriptRunnerLoadConfigMalformedJSON(t *testing.T) {
	repo := t.TempDir()
	writeWorktreeConfig(t, repo, `{invalid json}`)

	runner := NewScriptRunner(6200, 10)
	_, err := runner.LoadConfig(repo)
	if err == nil {
		t.Fatalf("LoadConfig() should fail for malformed JSON")
	}
}

func TestScriptRunnerLoadConfigValidJSON(t *testing.T) {
	repo := t.TempDir()
	writeWorktreeConfig(t, repo, `{
  "setup-worktree": ["echo setup1", "echo setup2"],
  "run": "npm start",
  "archive": "tar -czf archive.tar.gz ."
}`)

	runner := NewScriptRunner(6200, 10)
	cfg, err := runner.LoadConfig(repo)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.SetupWorktree) != 2 {
		t.Fatalf("expected 2 setup commands, got %d", len(cfg.SetupWorktree))
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
	worktreeRoot := t.TempDir()

	writeWorktreeConfig(t, repo, `{
  "setup-worktree": ["printf \"$AMUX_WORKTREE_NAME-$CUSTOM_VAR\" > setup.txt"]
}`)

	runner := NewScriptRunner(6200, 10)
	wt := &data.Worktree{
		Name:   "feature-1",
		Branch: "feature-1",
		Repo:   repo,
		Root:   worktreeRoot,
	}
	meta := &data.Metadata{
		Env: map[string]string{"CUSTOM_VAR": "hello"},
	}

	if err := runner.RunSetup(wt, meta); err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(worktreeRoot, "setup.txt"))
	if err != nil {
		t.Fatalf("expected setup.txt to exist: %v", err)
	}
	if strings.TrimSpace(string(contents)) != "feature-1-hello" {
		t.Fatalf("unexpected setup.txt contents: %s", contents)
	}
}

func TestScriptRunnerRunSetupFailure(t *testing.T) {
	repo := t.TempDir()
	worktreeRoot := t.TempDir()

	writeWorktreeConfig(t, repo, `{
  "setup-worktree": ["exit 1"]
}`)

	runner := NewScriptRunner(6200, 10)
	wt := &data.Worktree{Repo: repo, Root: worktreeRoot}

	if err := runner.RunSetup(wt, nil); err == nil {
		t.Fatalf("expected RunSetup() to fail for failing command")
	}
}

func TestScriptRunnerRunScriptConfigAndMeta(t *testing.T) {
	repo := t.TempDir()
	worktreeRoot := t.TempDir()

	writeWorktreeConfig(t, repo, `{
  "run": "printf run-config > run.txt"
}`)

	runner := NewScriptRunner(6200, 10)
	wt := &data.Worktree{Repo: repo, Root: worktreeRoot}

	_, err := runner.RunScript(wt, nil, ScriptRun)
	if err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}
	if err := waitForFile(filepath.Join(worktreeRoot, "run.txt"), 2*time.Second); err != nil {
		t.Fatalf("expected run.txt to be created: %v", err)
	}

	// Now test meta override when config missing.
	writeWorktreeConfig(t, repo, `{}`)
	meta := &data.Metadata{Scripts: data.ScriptsConfig{Run: "printf run-meta > run-meta.txt"}}
	_, err = runner.RunScript(wt, meta, ScriptRun)
	if err != nil {
		t.Fatalf("RunScript() meta override error = %v", err)
	}
	if err := waitForFile(filepath.Join(worktreeRoot, "run-meta.txt"), 2*time.Second); err != nil {
		t.Fatalf("expected run-meta.txt to be created: %v", err)
	}
}

func TestScriptRunnerRunScriptMissing(t *testing.T) {
	repo := t.TempDir()
	worktreeRoot := t.TempDir()

	writeWorktreeConfig(t, repo, `{}`)

	runner := NewScriptRunner(6200, 10)
	wt := &data.Worktree{Repo: repo, Root: worktreeRoot}

	if _, err := runner.RunScript(wt, nil, ScriptRun); err == nil {
		t.Fatalf("expected RunScript() to fail when no script configured")
	}
}

func TestScriptRunnerStop(t *testing.T) {
	repo := t.TempDir()
	worktreeRoot := t.TempDir()

	writeWorktreeConfig(t, repo, `{
  "run": "sleep 5"
}`)

	runner := NewScriptRunner(6200, 10)
	wt := &data.Worktree{Repo: repo, Root: worktreeRoot}

	if _, err := runner.RunScript(wt, nil, ScriptRun); err != nil {
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
