package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// mergeIntegrationRepo builds a real repo on main with a "feature" branch
// holding one commit, plus a data.Workspace describing that branch — the shape
// the dashboard hands to the merge action.
func mergeIntegrationRepo(t *testing.T) (repo string, ws *data.Workspace) {
	t.Helper()
	repo = testutil.InitRepo(t)

	testutil.RunGit(t, repo, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(repo, "feature.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatalf("write feature.txt: %v", err)
	}
	testutil.RunGit(t, repo, "add", "feature.txt")
	testutil.RunGit(t, repo, "commit", "-m", "feature work")
	testutil.RunGit(t, repo, "checkout", "main")

	return repo, data.NewWorkspace("feature", "feature", "origin/main", repo, filepath.Join(t.TempDir(), "feature"))
}

func newIntegrationMergeApp() *App {
	return &App{toast: common.NewToastModel(), config: &config.Config{}}
}

// TestMergeWorkspace_EndToEnd drives the real chain with no git seams: the
// dispatch routes the request, the precondition runs against a real checkout,
// the confirmed dialog runs a real `git merge --no-ff`, and the branch's commit
// lands on main.
func TestMergeWorkspace_EndToEnd(t *testing.T) {
	repo, ws := mergeIntegrationRepo(t)
	app := newIntegrationMergeApp()

	var cmds []tea.Cmd
	if !app.updateWorkspaceLifecycleMsg(messages.MergeWorkspace{Workspace: ws}, &cmds) {
		t.Fatal("MergeWorkspace is not routed by the workspace-lifecycle dispatch")
	}
	if len(cmds) != 1 {
		t.Fatalf("dispatch produced %d commands, want 1", len(cmds))
	}

	dialogReq, ok := cmds[0]().(messages.ShowMergeWorkspaceDialog)
	if !ok {
		t.Fatalf("merge produced %T, want ShowMergeWorkspaceDialog", cmds[0]())
	}
	if dialogReq.Base != "main" {
		t.Fatalf("resolved base = %q, want main", dialogReq.Base)
	}

	// Nothing has been written yet: the branch commit is still only on feature.
	if _, err := os.Stat(filepath.Join(repo, "feature.txt")); !os.IsNotExist(err) {
		t.Fatal("the merge wrote to the repo before the user confirmed")
	}

	app.handleShowMergeWorkspaceDialog(dialogReq)
	if app.dialog == nil || !app.dialog.Visible() {
		t.Fatal("the merge confirmation dialog was not shown")
	}

	mergeCmd := app.handleDialogResult(common.DialogResult{ID: DialogMergeWorkspace, Confirmed: true})
	if mergeCmd == nil {
		t.Fatal("confirming the dialog produced no merge command")
	}
	merged, ok := mergeCmd().(messages.WorkspaceMerged)
	if !ok {
		t.Fatalf("merge produced %T, want WorkspaceMerged", mergeCmd())
	}
	if merged.Err != nil {
		t.Fatalf("merge failed: %v", merged.Err)
	}

	// The branch's commit is now on main, recorded as a merge commit.
	if _, err := os.Stat(filepath.Join(repo, "feature.txt")); err != nil {
		t.Fatalf("feature.txt did not land on main: %v", err)
	}
	if got := testutil.RunGit(t, repo, "symbolic-ref", "--short", "HEAD"); got != "main" {
		t.Fatalf("HEAD moved to %q; the merge must land on the checked-out base", got)
	}
	parents := testutil.RunGit(t, repo, "rev-list", "--parents", "-n", "1", "HEAD")
	if len(strings.Fields(parents)) != 3 {
		t.Fatalf("merge did not record a merge commit: %q", parents)
	}
}

// TestMergeWorkspace_EndToEndResolvesBaseAgainstTheRepo covers base-ref
// resolution with no seams in the way. Both shapes must reach the confirm
// dialog: a remote-qualified base in a repo with no remote configured (stored
// metadata routinely outlives its remote), and a local default branch whose
// own name contains a slash.
func TestMergeWorkspace_EndToEndResolvesBaseAgainstTheRepo(t *testing.T) {
	cases := []struct {
		name       string
		baseBranch string
		storedBase string
		wantBase   string
	}{
		{"remote-qualified base, no remote configured", "main", "origin/main", "main"},
		{"local base branch containing a slash", "release/2.0", "release/2.0", "release/2.0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := testutil.InitRepoWithBranch(t, tc.baseBranch)
			testutil.RunGit(t, repo, "checkout", "-b", "feature")
			if err := os.WriteFile(filepath.Join(repo, "feature.txt"), []byte("work\n"), 0o600); err != nil {
				t.Fatalf("write feature.txt: %v", err)
			}
			testutil.RunGit(t, repo, "add", "feature.txt")
			testutil.RunGit(t, repo, "commit", "-m", "feature work")
			testutil.RunGit(t, repo, "checkout", tc.baseBranch)

			ws := data.NewWorkspace("feature", "feature", tc.storedBase, repo, filepath.Join(t.TempDir(), "feature"))
			app := newIntegrationMergeApp()

			cmd := app.handleMergeWorkspace(messages.MergeWorkspace{Workspace: ws})
			if cmd == nil {
				t.Fatal("merge produced no command")
			}
			dialogReq, ok := cmd().(messages.ShowMergeWorkspaceDialog)
			if !ok {
				t.Fatalf("merge was refused instead of confirming: %q", refusalReason(cmd))
			}
			if dialogReq.Base != tc.wantBase {
				t.Fatalf("resolved base = %q, want %q", dialogReq.Base, tc.wantBase)
			}
		})
	}
}

// TestMergeWorkspace_EndToEndRefusesWrongBranch asserts the precondition holds
// against a real repo: with the primary checkout parked on an unrelated branch,
// the action stops and the repository is untouched.
func TestMergeWorkspace_EndToEndRefusesWrongBranch(t *testing.T) {
	repo, ws := mergeIntegrationRepo(t)
	testutil.RunGit(t, repo, "checkout", "-b", "unrelated-work")
	headBefore := testutil.RunGit(t, repo, "rev-parse", "HEAD")

	app := newIntegrationMergeApp()
	cmd := app.handleMergeWorkspace(messages.MergeWorkspace{Workspace: ws})

	if reason := refusalReason(cmd); !strings.Contains(reason, "unrelated-work") {
		t.Fatalf("refusal = %q, want it to name the branch actually checked out", reason)
	}
	if got := testutil.RunGit(t, repo, "symbolic-ref", "--short", "HEAD"); got != "unrelated-work" {
		t.Fatalf("the refused merge moved HEAD to %q", got)
	}
	if after := testutil.RunGit(t, repo, "rev-parse", "HEAD"); after != headBefore {
		t.Fatal("the refused merge still advanced HEAD")
	}
	if _, err := os.Stat(filepath.Join(repo, "feature.txt")); !os.IsNotExist(err) {
		t.Fatal("the refused merge still wrote the branch's files")
	}
}

// TestMergeWorkspace_EndToEndConflictThenAbort exercises the conflict lifecycle
// against a real repo: the merge stops, amux lists the files and offers Abort,
// and confirming the abort restores the pre-merge state.
func TestMergeWorkspace_EndToEndConflictThenAbort(t *testing.T) {
	repo := testutil.InitRepo(t)
	write := func(content string) {
		if err := os.WriteFile(filepath.Join(repo, "shared.txt"), []byte(content), 0o600); err != nil {
			t.Fatalf("write shared.txt: %v", err)
		}
	}
	write("base\n")
	testutil.RunGit(t, repo, "add", "shared.txt")
	testutil.RunGit(t, repo, "commit", "-m", "add shared")

	testutil.RunGit(t, repo, "checkout", "-b", "feature")
	write("feature version\n")
	testutil.RunGit(t, repo, "commit", "-am", "feature edit")

	testutil.RunGit(t, repo, "checkout", "main")
	write("main version\n")
	testutil.RunGit(t, repo, "commit", "-am", "main edit")

	headBefore := testutil.RunGit(t, repo, "rev-parse", "HEAD")
	ws := data.NewWorkspace("feature", "feature", "origin/main", repo, filepath.Join(t.TempDir(), "feature"))
	app := newIntegrationMergeApp()

	app.handleShowMergeWorkspaceDialog(messages.ShowMergeWorkspaceDialog{Workspace: ws, Base: "main"})
	mergeCmd := app.handleDialogResult(common.DialogResult{ID: DialogMergeWorkspace, Confirmed: true})
	merged, ok := mergeCmd().(messages.WorkspaceMerged)
	if !ok {
		t.Fatalf("merge produced %T, want WorkspaceMerged", mergeCmd())
	}

	app.handleWorkspaceMerged(merged)
	if app.dialog == nil {
		t.Fatal("a real conflict did not open the conflict dialog")
	}
	if view := app.dialog.View(); !strings.Contains(view, "shared.txt") {
		t.Fatalf("conflict dialog does not list the conflicted file:\n%s", view)
	}

	abortCmd := app.handleDialogResult(common.DialogResult{ID: DialogMergeConflict, Confirmed: true})
	if abortCmd == nil {
		t.Fatal("confirming the conflict dialog produced no abort command")
	}
	aborted, ok := abortCmd().(messages.WorkspaceMergeAborted)
	if !ok {
		t.Fatalf("abort produced %T, want WorkspaceMergeAborted", abortCmd())
	}
	if aborted.Err != nil {
		t.Fatalf("abort failed: %v", aborted.Err)
	}

	if after := testutil.RunGit(t, repo, "rev-parse", "HEAD"); after != headBefore {
		t.Fatal("abort left HEAD moved")
	}
	if got := testutil.RunGit(t, repo, "status", "--porcelain"); got != "" {
		t.Fatalf("working tree not clean after abort: %q", got)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "MERGE_HEAD")); !os.IsNotExist(err) {
		t.Fatal("a merge is still in progress after the abort")
	}
}
