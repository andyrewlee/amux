package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// mergeWorkspace is the ordinary case: a feature workspace whose base is
// origin/main, in a repo whose primary checkout is on main.
func mergeWorkspace() *data.Workspace {
	return &data.Workspace{
		Name:   "feature",
		Branch: "feature",
		Base:   "origin/main",
		Repo:   "/repo",
		Root:   "/repo-workspaces/feature",
	}
}

// newMergeApp wires an App whose git calls are all seams, so the precondition
// and dialog logic can be exercised without a real repo. headBranch is what the
// primary checkout reports as its current branch.
func newMergeApp(headBranch string) *App {
	return &App{
		toast:  common.NewToastModel(),
		config: &config.Config{},
		checkedOutBranchFn: func(string) (string, error) {
			return headBranch, nil
		},
		// Stand in for git.LocalBaseBranch, whose real repo-querying behavior
		// is covered in internal/git and by the merge integration tests.
		localBaseBranchFn: func(_, base string) string {
			return strings.TrimPrefix(base, "origin/")
		},
	}
}

// firstMsgOfType returns the first message of type T produced by cmd.
func firstMsgOfType[T tea.Msg](cmd tea.Cmd) (T, bool) {
	var zero T
	for _, msg := range runCommandMessages(cmd) {
		if typed, ok := msg.(T); ok {
			return typed, true
		}
	}
	return zero, false
}

// visibleToast returns the text amux is currently showing the user.
// ToastModel.Show mutates the model and returns only a dismissal tick, so the
// message never appears in the command stream — the rendered view is where it
// actually lives, and is what the user reads.
func visibleToast(app *App) string {
	return ansi.Strip(app.toast.View())
}

// refusalReason runs cmd and returns the precondition refusal it produced, or
// "" if it produced none. Refusals travel as a message rather than a direct
// toast because the precondition runs off the UI goroutine.
func refusalReason(cmd tea.Cmd) string {
	if refused, ok := firstMsgOfType[messages.MergeWorkspaceRefused](cmd); ok {
		return refused.Reason
	}
	return ""
}

// TestHandleMergeWorkspace_OpensConfirmDialogWhenBaseCheckedOut asserts the
// happy path reaches a confirmation — and only a confirmation. Nothing is
// written before the user agrees.
func TestHandleMergeWorkspace_OpensConfirmDialogWhenBaseCheckedOut(t *testing.T) {
	app := newMergeApp("main")
	app.mergeBranchFn = func(context.Context, string, string) error {
		t.Fatal("merge ran before the user confirmed")
		return nil
	}

	cmd := app.handleMergeWorkspace(messages.MergeWorkspace{Workspace: mergeWorkspace()})
	dialog, ok := firstMsgOfType[messages.ShowMergeWorkspaceDialog](cmd)
	if !ok {
		t.Fatal("merging with the base checked out did not request the confirm dialog")
	}
	if dialog.Base != "main" {
		t.Fatalf("dialog base = %q, want the local base branch %q", dialog.Base, "main")
	}
}

// TestHandleMergeWorkspace_DefersGitToTheCommand asserts the precondition's git
// work happens inside the returned command, not inside Update. Running it inline
// would put up to three git subprocesses in the middle of the event loop,
// stalling rendering and input; every other git call in the app is deferred for
// the same reason.
func TestHandleMergeWorkspace_DefersGitToTheCommand(t *testing.T) {
	app := newMergeApp("main")

	var queriedInline bool
	inUpdate := true
	app.localBaseBranchFn = func(_, base string) string {
		if inUpdate {
			queriedInline = true
		}
		return strings.TrimPrefix(base, "origin/")
	}
	app.checkedOutBranchFn = func(string) (string, error) {
		if inUpdate {
			queriedInline = true
		}
		return "main", nil
	}

	cmd := app.handleMergeWorkspace(messages.MergeWorkspace{Workspace: mergeWorkspace()})
	if queriedInline {
		t.Fatal("the merge precondition shelled out to git on the UI goroutine")
	}
	if cmd == nil {
		t.Fatal("merge produced no command to run the precondition in")
	}

	// Running the command is where the queries belong.
	inUpdate = false
	if _, ok := firstMsgOfType[messages.ShowMergeWorkspaceDialog](cmd); !ok {
		t.Fatal("running the command did not resolve the precondition")
	}
}

// TestHandleMergeWorkspace_RefusesWhenBaseNotCheckedOut is the load-bearing
// safety property from the write-back design: amux must never move the primary
// checkout's HEAD to make a merge possible. If the user is working on another
// branch there, the action stops and says so.
func TestHandleMergeWorkspace_RefusesWhenBaseNotCheckedOut(t *testing.T) {
	app := newMergeApp("some-other-work")
	app.mergeBranchFn = func(context.Context, string, string) error {
		t.Fatal("merge ran even though the primary checkout was on the wrong branch")
		return nil
	}

	cmd := app.handleMergeWorkspace(messages.MergeWorkspace{Workspace: mergeWorkspace()})

	if _, ok := firstMsgOfType[messages.ShowMergeWorkspaceDialog](cmd); ok {
		t.Fatal("a confirm dialog was offered despite the failed precondition")
	}
	reason := refusalReason(cmd)
	if !strings.Contains(reason, "some-other-work") || !strings.Contains(reason, "main") {
		t.Fatalf("refusal %q should name both the current branch and the expected base", reason)
	}
}

// TestHandleMergeWorkspace_RefusesOnDetachedHead asserts a detached HEAD is
// refused rather than merged into "no branch".
func TestHandleMergeWorkspace_RefusesOnDetachedHead(t *testing.T) {
	app := newMergeApp("")
	app.checkedOutBranchFn = func(string) (string, error) {
		return "", errors.New("fatal: ref HEAD is not a symbolic ref")
	}

	cmd := app.handleMergeWorkspace(messages.MergeWorkspace{Workspace: mergeWorkspace()})
	if _, ok := firstMsgOfType[messages.ShowMergeWorkspaceDialog](cmd); ok {
		t.Fatal("a confirm dialog was offered for a detached HEAD")
	}
	refused, ok := firstMsgOfType[messages.MergeWorkspaceRefused](cmd)
	if !ok {
		t.Fatal("a detached HEAD produced no refusal")
	}
	// A check that could not run carries the cause, so it reports as an error
	// rather than a plain "no".
	if refused.Err == nil {
		t.Fatal("the detached-HEAD refusal dropped the underlying git error")
	}
	if _, isErr := firstMsgOfType[messages.Error](app.handleMergeWorkspaceRefused(refused)); !isErr {
		t.Fatal("a failed precondition check was not reported as an error")
	}
}

// TestHandleMergeWorkspace_RefusesUnmergeableWorkspaces covers the shapes that
// have nothing to merge, each of which would otherwise produce a confusing git
// error instead of a clear refusal.
func TestHandleMergeWorkspace_RefusesUnmergeableWorkspaces(t *testing.T) {
	cases := []struct {
		name string
		ws   *data.Workspace
		want string
	}{
		{
			name: "primary checkout",
			ws:   &data.Workspace{Name: "root", Branch: "main", Base: "origin/main", Repo: "/repo", Root: "/repo"},
			want: "primary checkout",
		},
		{
			name: "workspace already on the base branch",
			ws:   &data.Workspace{Name: "mainish", Branch: "main", Base: "origin/main", Repo: "/repo", Root: "/ws"},
			want: "base branch",
		},
		{
			name: "no branch recorded",
			ws:   &data.Workspace{Name: "odd", Branch: "", Base: "origin/main", Repo: "/repo", Root: "/ws"},
			want: "no branch",
		},
		{
			name: "no base recorded",
			ws:   &data.Workspace{Name: "odd", Branch: "feature", Base: "", Repo: "/repo", Root: "/ws"},
			want: "no recorded base",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := newMergeApp("main")
			app.mergeBranchFn = func(context.Context, string, string) error {
				t.Fatal("merge ran for a workspace that cannot be merged")
				return nil
			}

			cmd := app.handleMergeWorkspace(messages.MergeWorkspace{Workspace: tc.ws})

			if _, ok := firstMsgOfType[messages.ShowMergeWorkspaceDialog](cmd); ok {
				t.Fatal("a confirm dialog was offered for an unmergeable workspace")
			}
			if reason := refusalReason(cmd); !strings.Contains(reason, tc.want) {
				t.Fatalf("refusal = %q, want it to mention %q", reason, tc.want)
			}
		})
	}
}

// TestMergeConfirmDialog_RunsMergeWithVerifiedBase asserts a confirmed dialog
// runs the merge against the workspace's own repo and branch, reporting the
// base the user was actually shown.
func TestMergeConfirmDialog_RunsMergeWithVerifiedBase(t *testing.T) {
	ws := mergeWorkspace()
	var gotRepo, gotBranch string
	app := newMergeApp("main")
	app.mergeBranchFn = func(_ context.Context, repo, branch string) error {
		gotRepo, gotBranch = repo, branch
		return nil
	}

	app.handleShowMergeWorkspaceDialog(messages.ShowMergeWorkspaceDialog{Workspace: ws, Base: "main"})
	cmd := app.handleDialogResult(common.DialogResult{ID: DialogMergeWorkspace, Confirmed: true})
	if cmd == nil {
		t.Fatal("confirming the merge dialog produced no command")
	}
	merged, ok := cmd().(messages.WorkspaceMerged)
	if !ok {
		t.Fatalf("confirmed merge produced %T, want WorkspaceMerged", cmd())
	}

	if gotRepo != "/repo" || gotBranch != "feature" {
		t.Fatalf("merge ran as (repo=%q, branch=%q), want (/repo, feature)", gotRepo, gotBranch)
	}
	if merged.Base != "main" {
		t.Fatalf("WorkspaceMerged.Base = %q, want the verified base main", merged.Base)
	}
	if merged.Err != nil {
		t.Fatalf("unexpected merge error: %v", merged.Err)
	}
}

// TestMergeConfirmDialog_CancelDoesNotMerge is the counterpart: declining the
// dialog must leave the repository untouched.
func TestMergeConfirmDialog_CancelDoesNotMerge(t *testing.T) {
	app := newMergeApp("main")
	called := false
	app.mergeBranchFn = func(context.Context, string, string) error {
		called = true
		return nil
	}

	app.handleShowMergeWorkspaceDialog(messages.ShowMergeWorkspaceDialog{Workspace: mergeWorkspace(), Base: "main"})
	if cmd := app.handleDialogResult(common.DialogResult{ID: DialogMergeWorkspace, Confirmed: false}); cmd != nil {
		cmd()
	}
	if called {
		t.Fatal("declining the merge dialog still ran the merge")
	}
}

// TestHandleWorkspaceMerged_SuccessRefreshesPrimaryCheckout asserts the report
// side of a successful merge. What changed on disk is the primary checkout, not
// the workspace worktree, so its cached status must be dropped — otherwise the
// dashboard keeps showing the repo as it looked before amux wrote to it.
func TestHandleWorkspaceMerged_SuccessRefreshesPrimaryCheckout(t *testing.T) {
	ws := mergeWorkspace()
	app := newMergeApp("main")

	cmd := app.handleWorkspaceMerged(messages.WorkspaceMerged{
		Workspace: ws,
		Base:      "main",
	})
	if cmd == nil {
		t.Fatal("a successful merge produced no follow-up commands")
	}
	if toast := visibleToast(app); !strings.Contains(toast, "main") {
		t.Fatalf("success toast = %q, want it to name the base branch", toast)
	}
	if app.dialog != nil {
		t.Fatal("a successful merge opened a dialog")
	}

	// The refresh targets the primary checkout (Repo), not the worktree (Root).
	var refreshed string
	for _, msg := range runCommandMessages(cmd) {
		if status, ok := msg.(messages.GitStatusResult); ok {
			refreshed = status.Root
		}
	}
	if refreshed != ws.Repo {
		t.Fatalf("merge refreshed status for %q, want the primary checkout %q", refreshed, ws.Repo)
	}
}

// TestHandleWorkspaceMergeAborted_SuccessRefreshesPrimaryCheckout is the same
// property for abort: rewinding the merge changes the primary checkout's tree,
// so its mid-conflict cached status is stale.
func TestHandleWorkspaceMergeAborted_SuccessRefreshesPrimaryCheckout(t *testing.T) {
	ws := mergeWorkspace()
	app := newMergeApp("main")

	cmd := app.handleWorkspaceMergeAborted(messages.WorkspaceMergeAborted{Workspace: ws})
	if cmd == nil {
		t.Fatal("a successful abort produced no follow-up commands")
	}

	var refreshed string
	for _, msg := range runCommandMessages(cmd) {
		if status, ok := msg.(messages.GitStatusResult); ok {
			refreshed = status.Root
		}
	}
	if refreshed != ws.Repo {
		t.Fatalf("abort refreshed status for %q, want the primary checkout %q", refreshed, ws.Repo)
	}
}

// TestRefreshPrimaryCheckoutStatusIgnoresEmptyRepo asserts the helper is inert
// for a workspace with no recorded repo, rather than requesting a status for "".
func TestRefreshPrimaryCheckoutStatusIgnoresEmptyRepo(t *testing.T) {
	app := newMergeApp("main")
	if cmds := app.refreshPrimaryCheckoutStatus(""); cmds != nil {
		t.Fatalf("expected no commands for an empty repo path, got %d", len(cmds))
	}
}

// TestHandleWorkspaceMerged_ConflictOpensConflictDialog asserts a conflict gets
// its own actionable modal listing the files, not a raw error banner.
func TestHandleWorkspaceMerged_ConflictOpensConflictDialog(t *testing.T) {
	app := newMergeApp("main")
	ws := mergeWorkspace()

	cmd := app.handleWorkspaceMerged(messages.WorkspaceMerged{
		Workspace: ws,
		Base:      "main",
		Err: &git.MergeConflictError{
			Branch: "feature",
			Files:  []string{"a.go", "b.go"},
		},
	})
	if cmd != nil {
		if _, ok := firstMsgOfType[messages.Error](cmd); ok {
			t.Fatal("a conflict was reported as a generic error instead of the conflict dialog")
		}
	}

	if app.dialog == nil {
		t.Fatal("no conflict dialog was shown")
	}
	view := app.dialog.View()
	for _, want := range []string{"a.go", "b.go", "feature"} {
		if !strings.Contains(view, want) {
			t.Fatalf("conflict dialog does not mention %q:\n%s", want, view)
		}
	}
}

// TestConflictDialog_ConfirmAborts asserts the escape hatch is wired: saying
// yes to the conflict dialog aborts the merge.
func TestConflictDialog_ConfirmAborts(t *testing.T) {
	app := newMergeApp("main")
	var abortedIn string
	app.abortMergeFn = func(_ context.Context, repo string) error {
		abortedIn = repo
		return nil
	}

	app.showMergeConflictDialog(mergeWorkspace(), &git.MergeConflictError{
		Branch: "feature",
		Files:  []string{"a.go"},
	})
	cmd := app.handleDialogResult(common.DialogResult{ID: DialogMergeConflict, Confirmed: true})
	if cmd == nil {
		t.Fatal("confirming the conflict dialog produced no abort command")
	}
	if _, ok := cmd().(messages.WorkspaceMergeAborted); !ok {
		t.Fatalf("abort produced %T, want WorkspaceMergeAborted", cmd())
	}
	if abortedIn != "/repo" {
		t.Fatalf("abort ran in %q, want the primary checkout /repo", abortedIn)
	}
}

// TestConflictDialog_DeclineLeavesMergeInProgress asserts declining does
// nothing at all — the user resolves it themselves in the terminal.
func TestConflictDialog_DeclineLeavesMergeInProgress(t *testing.T) {
	app := newMergeApp("main")
	called := false
	app.abortMergeFn = func(context.Context, string) error {
		called = true
		return nil
	}

	app.showMergeConflictDialog(mergeWorkspace(), &git.MergeConflictError{Branch: "feature", Files: []string{"a.go"}})
	if cmd := app.handleDialogResult(common.DialogResult{ID: DialogMergeConflict, Confirmed: false}); cmd != nil {
		cmd()
	}
	if called {
		t.Fatal("declining the conflict dialog aborted the merge anyway")
	}
}

// TestHandleWorkspaceMergeAborted_FailureIsReported asserts a failed abort is
// escalated: the repository is still mid-merge and the user must know.
func TestHandleWorkspaceMergeAborted_FailureIsReported(t *testing.T) {
	app := newMergeApp("main")

	cmd := app.handleWorkspaceMergeAborted(messages.WorkspaceMergeAborted{
		Workspace: mergeWorkspace(),
		Err:       errors.New("no merge to abort"),
	})
	if _, ok := firstMsgOfType[messages.Error](cmd); !ok {
		t.Fatal("a failed abort was not reported")
	}
}

// TestFormatConflictList asserts long conflict lists are truncated with an
// explicit remainder rather than silently cut.
func TestFormatConflictList(t *testing.T) {
	if got := formatConflictList(nil); !strings.Contains(got, "no files") {
		t.Fatalf("empty list = %q, want an explicit no-files note", got)
	}

	many := make([]string, maxListedConflicts+5)
	for i := range many {
		many[i] = "file.go"
	}
	got := formatConflictList(many)
	if !strings.Contains(got, "and 5 more") {
		t.Fatalf("truncated list %q does not report the 5 omitted files", got)
	}
	if strings.Count(got, "file.go") != maxListedConflicts {
		t.Fatalf("listed %d files, want the cap of %d", strings.Count(got, "file.go"), maxListedConflicts)
	}
}
