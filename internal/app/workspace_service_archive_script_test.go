package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
)

// archiveDeleteFixture builds a managed workspace whose repo declares
// archiveCmd, wired to a real ScriptRunner. HOME is redirected at t.TempDir()
// first so the trust registry the runner resolves at construction is a
// throwaway, never the developer's real ~/.amux/trusted-scripts.json.
type archiveDeleteFixture struct {
	svc     *workspaceService
	project *data.Project
	ws      *data.Workspace
}

func newArchiveDeleteFixture(t *testing.T, archiveCmd string, trusted bool) archiveDeleteFixture {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "managed-workspaces")
	projectPath := filepath.Join(tmp, "repo")
	workspacePath := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath): %v", err)
	}

	if archiveCmd != "" {
		configDir := filepath.Join(projectPath, ".amux")
		if err := os.MkdirAll(configDir, 0o755); err != nil {
			t.Fatalf("MkdirAll(.amux): %v", err)
		}
		body := `{"archive": "` + strings.ReplaceAll(archiveCmd, `"`, `\"`) + `"}`
		if err := os.WriteFile(filepath.Join(configDir, "workspaces.json"), []byte(body), 0o644); err != nil {
			t.Fatalf("write workspaces.json: %v", err)
		}
	}

	scripts := process.NewScriptRunner(6600, 10)
	if trusted {
		if err := scripts.TrustRepoScripts(projectPath); err != nil {
			t.Fatalf("TrustRepoScripts: %v", err)
		}
	}

	svc := newWorkspaceService(nil, nil, scripts, workspacesRoot)
	svc.gitOps = &mockGitOps{
		removeWorkspace: func(string, string) error { return nil },
	}

	return archiveDeleteFixture{
		svc:     svc,
		project: data.NewProject(projectPath),
		ws:      data.NewWorkspace("feature", "feature", "main", projectPath, workspacePath),
	}
}

// TestDeleteWorkspace_RunsArchiveScriptBeforeWorktreeRemoval is the core
// contract: the archive script is a teardown hook, so it must complete while
// the worktree it operates on still exists.
func TestDeleteWorkspace_RunsArchiveScriptBeforeWorktreeRemoval(t *testing.T) {
	f := newArchiveDeleteFixture(t, "pwd > $AMUX_WORKSPACE_ROOT/archived.txt", true)

	msg := f.svc.DeleteWorkspace(f.project, f.ws)()
	deleted, ok := msg.(messages.WorkspaceDeleted)
	if !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T (%+v)", msg, msg)
	}
	if deleted.Warning != "" {
		t.Fatalf("unexpected warning from a successful archive script: %q", deleted.Warning)
	}

	// The marker proves the script both ran and saw a live worktree; the mock
	// gitOps leaves the directory in place so we can read it back.
	if _, err := os.Stat(filepath.Join(f.ws.Root, "archived.txt")); err != nil {
		t.Fatalf("archive script did not run during delete: %v", err)
	}
}

// TestDeleteWorkspace_ArchiveScriptFailureStillDeletes asserts a broken archive
// script degrades to a warning. A repo whose teardown hook is wrong must not be
// able to make its workspaces permanently undeletable.
func TestDeleteWorkspace_ArchiveScriptFailureStillDeletes(t *testing.T) {
	f := newArchiveDeleteFixture(t, "echo nope >&2; exit 1", true)

	msg := f.svc.DeleteWorkspace(f.project, f.ws)()
	deleted, ok := msg.(messages.WorkspaceDeleted)
	if !ok {
		t.Fatalf("a failing archive script blocked the delete: got %T (%+v)", msg, msg)
	}
	if !strings.Contains(deleted.Warning, "Archive script failed") {
		t.Fatalf("warning = %q, want it to report the archive-script failure", deleted.Warning)
	}
}

// TestDeleteWorkspace_UntrustedArchiveScriptIsSkipped asserts the trust gate
// holds on the delete path too, where no dialog can intervene: the command must
// not execute, and the user must be told it was skipped.
func TestDeleteWorkspace_UntrustedArchiveScriptIsSkipped(t *testing.T) {
	f := newArchiveDeleteFixture(t, "touch $AMUX_WORKSPACE_ROOT/should-not-exist.txt", false)

	msg := f.svc.DeleteWorkspace(f.project, f.ws)()
	deleted, ok := msg.(messages.WorkspaceDeleted)
	if !ok {
		t.Fatalf("an untrusted archive script blocked the delete: got %T (%+v)", msg, msg)
	}
	if !strings.Contains(deleted.Warning, "not trusted") {
		t.Fatalf("warning = %q, want it to say the script was skipped as untrusted", deleted.Warning)
	}
	if _, err := os.Stat(filepath.Join(f.ws.Root, "should-not-exist.txt")); !os.IsNotExist(err) {
		t.Fatal("untrusted archive command executed during delete; the trust gate did not hold")
	}
}

// TestDeleteWorkspace_NoArchiveScriptIsSilent asserts the common case — a repo
// with no archive script — deletes without inventing a warning.
func TestDeleteWorkspace_NoArchiveScriptIsSilent(t *testing.T) {
	f := newArchiveDeleteFixture(t, "", false)

	msg := f.svc.DeleteWorkspace(f.project, f.ws)()
	deleted, ok := msg.(messages.WorkspaceDeleted)
	if !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T (%+v)", msg, msg)
	}
	if deleted.Warning != "" {
		t.Fatalf("delete with no archive script warned anyway: %q", deleted.Warning)
	}
}

// TestJoinWarnings asserts a delete that produced both an archive warning and a
// branch-cleanup warning surfaces both, rather than one silently winning.
func TestJoinWarnings(t *testing.T) {
	cases := []struct {
		name  string
		input []string
		want  string
	}{
		{"none", []string{"", ""}, ""},
		{"first only", []string{"archive failed", ""}, "archive failed"},
		{"second only", []string{"", "branch not deleted"}, "branch not deleted"},
		{"both", []string{"archive failed", "branch not deleted"}, "archive failed; branch not deleted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := joinWarnings(tc.input...); got != tc.want {
				t.Fatalf("joinWarnings(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
