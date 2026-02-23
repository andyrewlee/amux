package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// skipIfNoSandboxExec skips the test if sandbox-exec is not available.
func skipIfNoSandboxExec(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available (non-macOS)")
	}
}

// skipIfNoGit skips the test if git is not installed.
func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

// sandboxEnv holds temp directories and the SBPL profile for integration tests.
type sandboxEnv struct {
	WorktreeRoot string
	GitDir       string
	ConfigDir    string
	OutsideDir   string // a directory outside the sandbox allowlist
	SBPLPath     string
}

// newSandboxEnv creates temporary directories and generates an SBPL profile.
// The "outside" directory is placed under $HOME (not under /private/var/folders)
// so it falls outside the sandbox's temp directory write allowance.
func newSandboxEnv(t *testing.T) *sandboxEnv {
	t.Helper()

	worktreeRoot := t.TempDir()
	gitDir := t.TempDir()
	configDir := t.TempDir()

	// Create outside dir under $HOME so it's not covered by the
	// (allow file-write* (subpath "/private/var/folders")) rule.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	outsideDir, err := os.MkdirTemp(home, ".medusa-sandbox-test-*")
	if err != nil {
		t.Fatalf("create outside dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(outsideDir) })

	sbpl := GenerateSBPL(worktreeRoot, gitDir, configDir)
	sbplPath, cleanup, sErr := WriteTempProfile(sbpl)
	if sErr != nil {
		t.Fatalf("WriteTempProfile: %v", sErr)
	}
	t.Cleanup(cleanup)

	return &sandboxEnv{
		WorktreeRoot: worktreeRoot,
		GitDir:       gitDir,
		ConfigDir:    configDir,
		OutsideDir:   outsideDir,
		SBPLPath:     sbplPath,
	}
}

// runSandboxed executes a shell command inside the sandbox and returns
// combined output and any error. The error is non-nil if the command exits
// with a non-zero status.
func runSandboxed(t *testing.T, sbplPath, command string) (string, error) {
	t.Helper()
	cmd := exec.Command("sandbox-exec", "-f", sbplPath, "sh", "-c", command)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// --- Tests that should SUCCEED ---

func TestSandbox_BasicShell(t *testing.T) {
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	out, err := runSandboxed(t, env.SBPLPath, "echo hello")
	if err != nil {
		t.Fatalf("echo should succeed in sandbox: %v\noutput: %s", err, out)
	}
	if out != "hello" {
		t.Errorf("expected 'hello', got %q", out)
	}
}

func TestSandbox_ReadInsideWorkspace(t *testing.T) {
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	testFile := filepath.Join(env.WorktreeRoot, "testfile.txt")
	if err := os.WriteFile(testFile, []byte("workspace content"), 0644); err != nil {
		t.Fatalf("setup: write test file: %v", err)
	}

	out, err := runSandboxed(t, env.SBPLPath, "cat "+testFile)
	if err != nil {
		t.Fatalf("reading workspace file should succeed: %v\noutput: %s", err, out)
	}
	if out != "workspace content" {
		t.Errorf("expected 'workspace content', got %q", out)
	}
}

func TestSandbox_WriteInsideWorkspace(t *testing.T) {
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	newFile := filepath.Join(env.WorktreeRoot, "newfile.txt")
	_, err := runSandboxed(t, env.SBPLPath, "touch "+newFile)
	if err != nil {
		t.Fatalf("writing inside workspace should succeed: %v", err)
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Error("file should exist after touch inside workspace")
	}
}

func TestSandbox_WriteTempAllowed(t *testing.T) {
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	tmpFile := "/tmp/medusa-sandbox-test-" + filepath.Base(env.WorktreeRoot)
	_, err := runSandboxed(t, env.SBPLPath, "touch "+tmpFile)
	if err != nil {
		t.Fatalf("writing to /tmp should succeed: %v", err)
	}
	t.Cleanup(func() { os.Remove(tmpFile) })

	if _, err := os.Stat(tmpFile); err != nil {
		t.Error("file should exist after touch in /tmp")
	}
}

func TestSandbox_WriteConfigDirAllowed(t *testing.T) {
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	configFile := filepath.Join(env.ConfigDir, "test-config.json")
	_, err := runSandboxed(t, env.SBPLPath, "touch "+configFile)
	if err != nil {
		t.Fatalf("writing to config dir should succeed: %v", err)
	}
	if _, err := os.Stat(configFile); err != nil {
		t.Error("file should exist after touch in config dir")
	}
}

func TestSandbox_DevWriteAllowed(t *testing.T) {
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	_, err := runSandboxed(t, env.SBPLPath, "echo test > /dev/null")
	if err != nil {
		t.Fatalf("writing to /dev/null should succeed: %v", err)
	}
}

func TestSandbox_RmInsideAllowed(t *testing.T) {
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	// Create a file to delete
	victim := filepath.Join(env.WorktreeRoot, "to-delete.txt")
	if err := os.WriteFile(victim, []byte("delete me"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err := runSandboxed(t, env.SBPLPath, "rm "+victim)
	if err != nil {
		t.Fatalf("rm inside workspace should succeed: %v", err)
	}
	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Error("file should be deleted after rm inside workspace")
	}
}

func TestSandbox_RmRfInsideAllowed(t *testing.T) {
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	// Create a directory tree to delete
	dir := filepath.Join(env.WorktreeRoot, "subdir", "nested")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("nested"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	target := filepath.Join(env.WorktreeRoot, "subdir")
	_, err := runSandboxed(t, env.SBPLPath, "rm -rf "+target)
	if err != nil {
		t.Fatalf("rm -rf inside workspace should succeed: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("directory should be deleted after rm -rf inside workspace")
	}
}

func TestSandbox_GitOperations(t *testing.T) {
	skipIfNoSandboxExec(t)
	skipIfNoGit(t)

	// Set up a real git repo as the worktree root with a matching gitDir
	worktreeRoot := t.TempDir()
	configDir := t.TempDir()

	// Initialize a git repo
	initGit(t, worktreeRoot)

	gitDir := filepath.Join(worktreeRoot, ".git")
	sbpl := GenerateSBPL(worktreeRoot, gitDir, configDir)
	sbplPath, cleanup, err := WriteTempProfile(sbpl)
	if err != nil {
		t.Fatalf("WriteTempProfile: %v", err)
	}
	t.Cleanup(cleanup)

	t.Run("git_status", func(t *testing.T) {
		out, err := runSandboxed(t, sbplPath, fmt.Sprintf("cd %s && git status", worktreeRoot))
		if err != nil {
			t.Fatalf("git status should succeed: %v\noutput: %s", err, out)
		}
	})

	t.Run("git_commit", func(t *testing.T) {
		// Create a file and commit it
		testFile := filepath.Join(worktreeRoot, "test.txt")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			t.Fatalf("setup: %v", err)
		}

		cmd := fmt.Sprintf(
			"cd %s && git add test.txt && git -c user.name=Test -c user.email=test@test.com commit -m 'test commit'",
			worktreeRoot,
		)
		out, err := runSandboxed(t, sbplPath, cmd)
		if err != nil {
			t.Fatalf("git commit should succeed: %v\noutput: %s", err, out)
		}
	})
}

func TestSandbox_GitCommitWorktree(t *testing.T) {
	skipIfNoSandboxExec(t)
	skipIfNoGit(t)

	// Set up a real worktree where gitDir is separate from worktreeRoot.
	// This verifies the gitDir allowance works independently.
	repo := t.TempDir()
	initGit(t, repo)

	worktreePath := filepath.Join(t.TempDir(), "wt")
	runGit(t, repo, "worktree", "add", "-b", "sandbox-test", worktreePath, "HEAD")

	// The main repo's .git dir is separate from the worktree
	gitDir := filepath.Join(repo, ".git")
	configDir := t.TempDir()

	sbpl := GenerateSBPL(worktreePath, gitDir, configDir)
	sbplPath, cleanup, err := WriteTempProfile(sbpl)
	if err != nil {
		t.Fatalf("WriteTempProfile: %v", err)
	}
	t.Cleanup(cleanup)

	// Create a file in the worktree and commit — this writes to gitDir
	// (refs, objects) which is outside worktreePath
	testFile := filepath.Join(worktreePath, "worktree-file.txt")
	if err := os.WriteFile(testFile, []byte("from worktree"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	cmd := fmt.Sprintf(
		"cd %s && git add worktree-file.txt && git -c user.name=Test -c user.email=test@test.com commit -m 'worktree commit'",
		worktreePath,
	)
	out, err := runSandboxed(t, sbplPath, cmd)
	if err != nil {
		t.Fatalf("git commit in worktree should succeed (gitDir allowance): %v\noutput: %s", err, out)
	}
}

// --- Tests that should FAIL (sandbox blocks the operation) ---

func TestSandbox_WriteOutsideBlocked(t *testing.T) {
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	outsideFile := filepath.Join(env.OutsideDir, "should-not-exist.txt")
	_, err := runSandboxed(t, env.SBPLPath, "touch "+outsideFile)
	if err == nil {
		t.Error("writing outside sandbox should be blocked")
		os.Remove(outsideFile) // cleanup if it was created
	}
}

func TestSandbox_ReadSensitiveSSH(t *testing.T) {
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	home, _ := os.UserHomeDir()
	sshDir := filepath.Join(home, ".ssh")
	if _, err := os.Stat(sshDir); os.IsNotExist(err) {
		t.Skip("~/.ssh does not exist")
	}

	_, err := runSandboxed(t, env.SBPLPath, "ls "+sshDir)
	if err == nil {
		t.Error("reading ~/.ssh should be blocked")
	}
}

func TestSandbox_ReadSensitiveAWS(t *testing.T) {
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	home, _ := os.UserHomeDir()
	awsDir := filepath.Join(home, ".aws")
	if _, err := os.Stat(awsDir); os.IsNotExist(err) {
		t.Skip("~/.aws does not exist")
	}

	_, err := runSandboxed(t, env.SBPLPath, "ls "+awsDir)
	if err == nil {
		t.Error("reading ~/.aws should be blocked")
	}
}

func TestSandbox_WriteEtcBlocked(t *testing.T) {
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	_, err := runSandboxed(t, env.SBPLPath, "touch /etc/medusa-sandbox-test")
	if err == nil {
		t.Error("writing to /etc should be blocked")
		os.Remove("/etc/medusa-sandbox-test")
	}
}

func TestSandbox_RmOutsideBlocked(t *testing.T) {
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	// Create a file outside the sandbox to try to delete
	victim := filepath.Join(env.OutsideDir, "protected-file.txt")
	if err := os.WriteFile(victim, []byte("do not delete"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err := runSandboxed(t, env.SBPLPath, "rm "+victim)
	if err == nil {
		t.Error("rm outside sandbox should be blocked")
	}
	// Verify file still exists
	if _, err := os.Stat(victim); err != nil {
		t.Error("file outside sandbox should still exist after blocked rm")
	}
}

func TestSandbox_RmRfOutsideBlocked(t *testing.T) {
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	// Create a directory tree outside the sandbox to try to delete
	dir := filepath.Join(env.OutsideDir, "protected-dir", "nested")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "important.txt"), []byte("critical data"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	target := filepath.Join(env.OutsideDir, "protected-dir")
	_, err := runSandboxed(t, env.SBPLPath, "rm -rf "+target)
	if err == nil {
		t.Error("rm -rf outside sandbox should be blocked")
	}
	// Verify directory still exists
	if _, err := os.Stat(target); err != nil {
		t.Error("directory outside sandbox should still exist after blocked rm -rf")
	}
	// Verify nested file still exists
	if _, err := os.Stat(filepath.Join(dir, "important.txt")); err != nil {
		t.Error("nested file outside sandbox should still exist after blocked rm -rf")
	}
}

// --- Helpers ---

// runGit executes a git command in dir with a clean environment.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Filter GIT_ env vars that may leak from hooks
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_DIR=") &&
			!strings.HasPrefix(e, "GIT_WORK_TREE=") &&
			!strings.HasPrefix(e, "GIT_INDEX_FILE=") {
			env = append(env, e)
		}
	}
	cmd.Env = append(env,
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// initGit initializes a git repo with an initial commit.
func initGit(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "config", "user.email", "test@test.com")

	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("init"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "init")
}
