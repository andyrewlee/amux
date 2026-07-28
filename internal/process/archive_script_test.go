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

// newArchiveWorkspace builds a repo + workspace pair whose .amux/workspaces.json
// declares archiveCmd, with trust already granted so the command actually runs.
func newArchiveWorkspace(t *testing.T, archiveCmd string) (*ScriptRunner, *data.Workspace) {
	t.Helper()
	repo := t.TempDir()
	wsRoot := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"archive": `+quoteJSON(archiveCmd)+`}`)

	runner := NewScriptRunner(6400, 10)
	trustRepo(t, runner, repo)

	return runner, &data.Workspace{
		Name:   "archive-me",
		Branch: "archive-me",
		Repo:   repo,
		Root:   wsRoot,
	}
}

// quoteJSON renders s as a JSON string literal for embedding in test configs.
func quoteJSON(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// TestRunArchiveRunsToCompletion is the property the delete path depends on:
// RunArchive must not return until the script has finished, because the caller
// removes the worktree immediately afterwards.
func TestRunArchiveRunsToCompletion(t *testing.T) {
	runner, ws := newArchiveWorkspace(t, "sleep 0.2; echo done > archived.txt")

	if err := runner.RunArchive(ws); err != nil {
		t.Fatalf("RunArchive: unexpected error: %v", err)
	}

	// The file must exist the instant RunArchive returns; if RunArchive were
	// async this read would race and usually lose.
	got, err := os.ReadFile(filepath.Join(ws.Root, "archived.txt"))
	if err != nil {
		t.Fatalf("archive script output missing after RunArchive returned: %v", err)
	}
	if strings.TrimSpace(string(got)) != "done" {
		t.Fatalf("archived.txt = %q, want %q", got, "done")
	}
}

// TestRunArchiveRunsInWorkspaceRoot pins the working directory: the archive
// script packages the workspace, so it must run in the worktree, not the repo.
func TestRunArchiveRunsInWorkspaceRoot(t *testing.T) {
	runner, ws := newArchiveWorkspace(t, "pwd > where.txt")

	if err := runner.RunArchive(ws); err != nil {
		t.Fatalf("RunArchive: unexpected error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(ws.Root, "where.txt"))
	if err != nil {
		t.Fatalf("read where.txt: %v", err)
	}
	// macOS temp dirs are symlinked (/var -> /private/var), so compare the
	// resolved paths rather than the literal strings.
	wantDir, err := filepath.EvalSymlinks(ws.Root)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", ws.Root, err)
	}
	gotDir, err := filepath.EvalSymlinks(strings.TrimSpace(string(got)))
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", got, err)
	}
	if gotDir != wantDir {
		t.Fatalf("archive script ran in %q, want %q", gotDir, wantDir)
	}
}

// TestRunArchiveReportsFailureWithStderr asserts a failing script surfaces both
// a non-nil error and the script's own stderr, which is what the delete-path
// warning shows the user.
func TestRunArchiveReportsFailureWithStderr(t *testing.T) {
	runner, ws := newArchiveWorkspace(t, "echo boom >&2; exit 3")

	err := runner.RunArchive(ws)
	if err == nil {
		t.Fatal("RunArchive: expected an error for a script exiting non-zero")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("RunArchive error %q does not include the script's stderr", err)
	}
}

// TestRunArchiveNoScriptConfigured asserts the benign case is distinguishable:
// most repos define no archive script and deleting their workspaces must not
// warn.
func TestRunArchiveNoScriptConfigured(t *testing.T) {
	repo := t.TempDir()
	runner := NewScriptRunner(6400, 10)
	useTempTrust(t, runner)
	ws := &data.Workspace{Name: "plain", Repo: repo, Root: t.TempDir()}

	err := runner.RunArchive(ws)
	if !errors.Is(err, ErrNoScriptConfigured) {
		t.Fatalf("RunArchive with no archive script: got %v, want ErrNoScriptConfigured", err)
	}
}

// TestRunArchiveUntrustedRepoRunsNothing is the security property: a
// repo-supplied archive command must not execute before the user has approved
// the config, even on the delete path where no dialog is in the way.
func TestRunArchiveUntrustedRepoRunsNothing(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"archive": "touch should-not-exist.txt"}`)

	runner := NewScriptRunner(6400, 10)
	useTempTrust(t, runner) // isolated registry, trust deliberately NOT granted
	ws := &data.Workspace{Name: "untrusted", Repo: repo, Root: wsRoot}

	err := runner.RunArchive(ws)
	if !errors.Is(err, ErrScriptsNotTrusted) {
		t.Fatalf("RunArchive on an untrusted repo: got %v, want ErrScriptsNotTrusted", err)
	}
	if _, statErr := os.Stat(filepath.Join(wsRoot, "should-not-exist.txt")); !os.IsNotExist(statErr) {
		t.Fatal("untrusted archive command executed; the trust gate did not hold")
	}
}

// TestRunArchiveUserEnteredScriptIsNotGated is the complement: a command the
// user typed into amux themselves is their own input and runs without approval,
// matching RunScript's documented split.
func TestRunArchiveUserEnteredScriptIsNotGated(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()

	runner := NewScriptRunner(6400, 10)
	useTempTrust(t, runner) // no trust granted for the repo
	ws := &data.Workspace{Name: "own-script", Repo: repo, Root: wsRoot}
	ws.Scripts.Archive = "touch user-archive.txt"

	if err := runner.RunArchive(ws); err != nil {
		t.Fatalf("RunArchive with a user-entered script: unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wsRoot, "user-archive.txt")); err != nil {
		t.Fatalf("user-entered archive script did not run: %v", err)
	}
}

// TestRunArchiveTimesOut asserts a hanging archive script cannot block a delete
// forever: the timeout fires, the process group is killed, and RunArchive
// returns rather than waiting on a script that never exits.
func TestRunArchiveTimesOut(t *testing.T) {
	original := archiveTimeout
	archiveTimeout = 150 * time.Millisecond
	t.Cleanup(func() { archiveTimeout = original })

	runner, ws := newArchiveWorkspace(t, "sleep 60")

	start := time.Now()
	err := runner.RunArchive(ws)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("RunArchive: expected a timeout error for a script that never exits")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("RunArchive error = %q, want a timeout error", err)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("RunArchive took %s; the timeout did not interrupt the script", elapsed)
	}
}

// TestRunArchiveReturnsWhenTheProcessCannotBeKilled is the "a delete must never
// hang" guarantee. With a killer that does nothing — standing in for a process
// wedged where signals cannot reach it — RunArchive must still give up and
// return rather than waiting forever on a process that will not be reaped.
func TestRunArchiveReturnsWhenTheProcessCannotBeKilled(t *testing.T) {
	originalTimeout := archiveTimeout
	archiveTimeout = 100 * time.Millisecond
	originalStop := scriptStopTimeout
	scriptStopTimeout = 100 * time.Millisecond
	t.Cleanup(func() {
		archiveTimeout = originalTimeout
		scriptStopTimeout = originalStop
	})

	runner, ws := newArchiveWorkspace(t, "sleep 60")
	var killAttempts int
	runner.killProcessGroup = func(int, KillOptions) error {
		killAttempts++
		return nil // deliberately ineffective
	}
	// The real process is still ours to clean up once the test is done.
	t.Cleanup(runner.StopAll)

	done := make(chan error, 1)
	go func() { done <- runner.RunArchive(ws) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("RunArchive: expected a timeout error")
		}
		if !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("RunArchive error = %q, want a timeout error", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("RunArchive never returned; an unkillable archive script can hang a workspace delete")
	}

	if killAttempts == 0 {
		t.Fatal("RunArchive did not try to kill the timed-out script")
	}
}

// TestRunArchiveClearsRunningEntry asserts the workspace is not left marked as
// having a live script after the archive finishes — a stale entry would make
// the subsequent port release think a script is still running.
func TestRunArchiveClearsRunningEntry(t *testing.T) {
	runner, ws := newArchiveWorkspace(t, "true")

	if err := runner.RunArchive(ws); err != nil {
		t.Fatalf("RunArchive: unexpected error: %v", err)
	}
	if runner.IsRunning(ws) {
		t.Fatal("workspace still marked as running after RunArchive returned")
	}
}
