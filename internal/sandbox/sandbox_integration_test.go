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

// --- OAuth / credential tests ---
// These tests verify whether the sandbox profile allows or blocks operations
// that Claude Code needs for OAuth authentication.

func TestSandbox_OAuthWriteToConfigDir(t *testing.T) {
	// Claude Code stores credentials under CLAUDE_CONFIG_DIR when set.
	// The sandbox must allow writes to the config dir for OAuth to work.
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	// Simulate Claude writing OAuth credentials to its config dir
	credFile := filepath.Join(env.ConfigDir, "credentials.json")
	cmd := fmt.Sprintf(`echo '{"oauth_token":"test"}' > %s`, credFile)
	out, err := runSandboxed(t, env.SBPLPath, cmd)
	if err != nil {
		t.Fatalf("writing credentials to CLAUDE_CONFIG_DIR should succeed: %v\noutput: %s", err, out)
	}

	data, err := os.ReadFile(credFile)
	if err != nil {
		t.Fatalf("credential file should exist: %v", err)
	}
	if !strings.Contains(string(data), "oauth_token") {
		t.Error("credential file should contain the written token")
	}
}

func TestSandbox_OAuthWriteToClaudeHomeBlocked(t *testing.T) {
	// If Claude Code ignores CLAUDE_CONFIG_DIR for some operations and
	// tries to write directly to ~/.claude/, the sandbox will block it.
	// This test documents that behavior.
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	home, _ := os.UserHomeDir()

	// Create a temp dir under ~/.claude-sandbox-test to simulate ~/.claude
	// (we don't want to touch the real ~/.claude)
	testClaudeDir := filepath.Join(home, ".claude-sandbox-oauth-test")
	if err := os.MkdirAll(testClaudeDir, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(testClaudeDir) })

	credFile := filepath.Join(testClaudeDir, "credentials.json")
	cmd := fmt.Sprintf(`echo '{"token":"test"}' > %s`, credFile)
	_, err := runSandboxed(t, env.SBPLPath, cmd)
	if err == nil {
		t.Error("writing to arbitrary home directory path should be BLOCKED by sandbox")
		os.Remove(credFile)
	}
}

func TestSandbox_OAuthReadClaudeHome(t *testing.T) {
	// Reading ~/.claude/ should work (global file-read* is allowed).
	// This matters for reading existing credentials/config.
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	home, _ := os.UserHomeDir()

	// Create a test file simulating existing credentials
	testClaudeDir := filepath.Join(home, ".claude-sandbox-read-test")
	if err := os.MkdirAll(testClaudeDir, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(testClaudeDir) })

	testFile := filepath.Join(testClaudeDir, "config.json")
	if err := os.WriteFile(testFile, []byte(`{"existing":"config"}`), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	out, err := runSandboxed(t, env.SBPLPath, "cat "+testFile)
	if err != nil {
		t.Fatalf("reading config files outside sandbox should succeed (file-read* is global): %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "existing") {
		t.Error("should be able to read existing config files")
	}
}

func TestSandbox_OAuthLocalhostListen(t *testing.T) {
	// OAuth flow requires listening on localhost for the callback.
	// The sandbox allows network* so this should work.
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	// Use Python to bind a socket briefly on localhost, then exit
	cmd := `python3 -c "
import socket, sys
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
try:
    s.bind(('127.0.0.1', 0))
    s.listen(1)
    port = s.getsockname()[1]
    print(f'listening on {port}')
    s.close()
except Exception as e:
    print(f'error: {e}', file=sys.stderr)
    sys.exit(1)
"`
	out, err := runSandboxed(t, env.SBPLPath, cmd)
	if err != nil {
		t.Fatalf("localhost listen should succeed (network* allowed): %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "listening on") {
		t.Errorf("expected 'listening on <port>', got %q", out)
	}
}

func TestSandbox_OAuthOutboundHTTPS(t *testing.T) {
	// OAuth requires outbound HTTPS to the auth server.
	// The sandbox allows network* so this should work.
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	// Use curl to make a simple HTTPS request (just check DNS + TLS works)
	out, err := runSandboxed(t, env.SBPLPath, "curl -sf -o /dev/null -w '%{http_code}' https://api.anthropic.com/ 2>&1 || true")
	if err != nil {
		t.Logf("curl attempt output: %s (err: %v)", out, err)
		// Even a connection error is fine — we're testing that the sandbox
		// doesn't block the network operation itself
	}

	// Alternative: just verify DNS resolution works
	out2, err2 := runSandboxed(t, env.SBPLPath, "python3 -c \"import socket; print(socket.getaddrinfo('api.anthropic.com', 443)[0][4][0])\"")
	if err2 != nil {
		t.Fatalf("DNS resolution should succeed (network* allowed): %v\noutput: %s", err2, out2)
	}
}

func TestSandbox_OAuthWriteClaudeJSON(t *testing.T) {
	// Claude Code may write to ~/.claude.json (the top-level config).
	// With CLAUDE_CONFIG_DIR set, it should write to $CLAUDE_CONFIG_DIR/.claude.json instead.
	// But if it falls back to ~/.claude.json, the sandbox will block it.
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	home, _ := os.UserHomeDir()

	// Test 1: Writing to $CLAUDE_CONFIG_DIR/.claude.json should SUCCEED
	configJSON := filepath.Join(env.ConfigDir, ".claude.json")
	cmd := fmt.Sprintf(`echo '{"projects":{}}' > %s`, configJSON)
	out, err := runSandboxed(t, env.SBPLPath, cmd)
	if err != nil {
		t.Errorf("writing .claude.json inside CLAUDE_CONFIG_DIR should succeed: %v\noutput: %s", err, out)
	}

	// Test 2: Writing to ~/.claude.json should be BLOCKED
	// Use a sentinel file so we don't corrupt the real ~/.claude.json
	sentinelPath := filepath.Join(home, ".claude-sandbox-test-sentinel.json")
	cmd2 := fmt.Sprintf(`echo '{"test":true}' > %s`, sentinelPath)
	_, err2 := runSandboxed(t, env.SBPLPath, cmd2)
	if err2 == nil {
		t.Error("writing to ~/<dotfile>.json outside sandbox allowlist should be BLOCKED")
		os.Remove(sentinelPath)
	}
}

func TestSandbox_OAuthKeychainAccess(t *testing.T) {
	// Claude Code may use the macOS Keychain for credential storage.
	// The sandbox allows mach-lookup which covers most XPC services,
	// but we should verify security framework access works.
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	// Try to list keychain items (read-only, non-destructive)
	// security find-generic-password just queries — if sandbox blocks Keychain
	// XPC, we'll see a specific error
	out, err := runSandboxed(t, env.SBPLPath, "security list-keychains 2>&1")
	if err != nil {
		t.Errorf("Keychain access may be blocked by sandbox: %v\noutput: %s", err, out)
		t.Log("If Claude Code uses Keychain for OAuth tokens, the sandbox could break authentication")
	} else {
		t.Logf("Keychain access works: %s", out)
	}
}

func TestSandbox_OAuthConfigLockDir(t *testing.T) {
	// Claude Code creates a lock directory as a sibling of the config dir
	// (e.g. "Work.lock" next to "Work/") to acquire a config lock before
	// writing credentials. The sandbox must allow this.
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	lockDir := env.ConfigDir + ".lock"
	cmd := fmt.Sprintf("mkdir -p %s && touch %s/test.lock", lockDir, lockDir)
	out, err := runSandboxed(t, env.SBPLPath, cmd)
	if err != nil {
		t.Fatalf("creating config lock dir should succeed: %v\noutput: %s", err, out)
	}
	t.Cleanup(func() { os.RemoveAll(lockDir) })

	if _, err := os.Stat(filepath.Join(lockDir, "test.lock")); err != nil {
		t.Error("lock file should exist after creation")
	}
}

func TestSandbox_OAuthClaudeStateDir(t *testing.T) {
	// Claude Code writes version locks to ~/.local/state/claude/locks/.
	// The sandbox must allow writes there.
	skipIfNoSandboxExec(t)
	env := newSandboxEnv(t)

	home, _ := os.UserHomeDir()
	stateDir := filepath.Join(home, ".local", "state", "claude", "locks")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	lockFile := filepath.Join(stateDir, "sandbox-test.lock")
	cmd := fmt.Sprintf("touch %s", lockFile)
	out, err := runSandboxed(t, env.SBPLPath, cmd)
	if err != nil {
		t.Fatalf("writing to ~/.local/state/claude should succeed: %v\noutput: %s", err, out)
	}
	t.Cleanup(func() { os.Remove(lockFile) })

	if _, err := os.Stat(lockFile); err != nil {
		t.Error("lock file should exist after creation in claude state dir")
	}
}

func TestSandbox_OAuthNoProfile_WriteClaudeHomeFails(t *testing.T) {
	// When a workspace is isolated but has NO profile (CLAUDE_CONFIG_DIR unset),
	// Claude Code falls back to ~/.claude/ and ~/.claude.json for credential storage.
	// The sandbox blocks all writes there because claudeConfigDir="" means no
	// config dir is in the write allowlist.
	//
	// This is the primary way the sandbox can break OAuth.
	skipIfNoSandboxExec(t)

	home, _ := os.UserHomeDir()
	worktreeRoot := t.TempDir()
	gitDir := ""

	// Simulate no profile: empty claudeConfigDir
	sbpl := GenerateSBPL(worktreeRoot, gitDir, "")
	sbplPath, cleanup, err := WriteTempProfile(sbpl)
	if err != nil {
		t.Fatalf("WriteTempProfile: %v", err)
	}
	t.Cleanup(cleanup)

	// Claude Code would try to write OAuth tokens to ~/.claude/credentials.json
	testDir := filepath.Join(home, ".claude-sandbox-no-profile-test")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(testDir) })

	credFile := filepath.Join(testDir, "credentials.json")
	cmd := fmt.Sprintf(`echo '{"oauth_token":"test"}' > %s`, credFile)
	_, err = runSandboxed(t, sbplPath, cmd)
	if err == nil {
		t.Error("BUG: with no profile, sandbox allows writing to home — OAuth token writes should be blocked")
		os.Remove(credFile)
		return
	}

	// This confirms the problem: no profile + isolated = can't store OAuth creds
	t.Log("Confirmed: sandbox blocks credential writes when no profile is set (no CLAUDE_CONFIG_DIR)")
	t.Log("This breaks OAuth because Claude Code can't persist tokens to ~/.claude/")
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
