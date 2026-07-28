package app

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// maxListedConflicts caps how many conflicted paths the modal spells out, so a
// merge that conflicts across hundreds of files still produces a dialog that
// fits on screen. The remainder is summarized rather than silently dropped.
const maxListedConflicts = 10

// handleMergeWorkspace runs the merge precondition and, if it holds, opens the
// confirm dialog. Nothing is written here — this is the "can we, and does the
// user agree?" half.
//
// The precondition is the load-bearing safety check from the write-back design:
// merge mutates the project's primary checkout, and amux must never check the
// base branch out on the user's behalf. So it verifies the primary checkout is
// *already* on the base branch and refuses otherwise, rather than moving HEAD
// to make the merge possible. That protects the common case of a user working
// directly on main in the primary checkout.
func (a *App) handleMergeWorkspace(msg messages.MergeWorkspace) tea.Cmd {
	ws := msg.Workspace
	if ws == nil {
		return nil
	}

	// These three need no git, so they answer immediately from stored metadata.
	var reason string
	switch {
	case ws.IsPrimaryCheckout():
		reason = "Cannot merge the primary checkout into itself"
	case ws.IsMainBranch():
		reason = fmt.Sprintf("Workspace '%s' is on a base branch; nothing to merge", ws.Name)
	case strings.TrimSpace(ws.Branch) == "":
		reason = fmt.Sprintf("Workspace '%s' has no branch to merge", ws.Name)
	}
	if reason != "" {
		return refuseMerge(ws, reason)
	}

	return a.resolveMergePreconditionAsync(ws)
}

// resolveMergePreconditionAsync answers the two questions that need git — which
// local branch the stored base ref names, and what the primary checkout
// actually has checked out — off the UI goroutine.
//
// Doing this inline would put up to three git subprocesses in the middle of
// Update, stalling rendering and input for as long as they take; on a large or
// network-backed repository that is plainly visible. Every other git call in
// the app is behind a tea.Cmd for the same reason.
//
// Both queries go through a seam so unit tests can drive the decision logic
// without building a real repo. The seams are set once at construction and
// never mutated, so reading them from the command goroutine is safe. Their real
// behavior is covered against real repos in internal/git and by the merge
// integration tests.
func (a *App) resolveMergePreconditionAsync(ws *data.Workspace) tea.Cmd {
	resolveBase := a.localBaseBranch
	resolveHead := a.checkedOutBranch

	return func() tea.Msg {
		base := resolveBase(ws.Repo, ws.Base)
		switch {
		case base == "":
			return refusal(ws, fmt.Sprintf("Workspace '%s' has no recorded base branch", ws.Name))
		case base == ws.Branch:
			return refusal(ws, "Workspace branch and base branch are the same")
		}

		head, err := resolveHead(ws.Repo)
		if err != nil {
			return messages.MergeWorkspaceRefused{
				Workspace: ws,
				Reason:    "Cannot merge: the primary checkout has no branch checked out (detached HEAD)",
				Err:       err,
			}
		}
		if head != base {
			return refusal(ws, fmt.Sprintf(
				"Cannot merge: %s is on '%s', not the base '%s'. Check out '%s' there and retry.",
				ws.Repo, head, base, base))
		}

		return messages.ShowMergeWorkspaceDialog{Workspace: ws, Base: base}
	}
}

// refusal builds the "precondition said no" message; refuseMerge wraps it as a
// command for the callers that are already on the UI goroutine.
func refusal(ws *data.Workspace, reason string) messages.MergeWorkspaceRefused {
	return messages.MergeWorkspaceRefused{Workspace: ws, Reason: reason}
}

func refuseMerge(ws *data.Workspace, reason string) tea.Cmd {
	return func() tea.Msg { return refusal(ws, reason) }
}

// handleMergeWorkspaceRefused surfaces a precondition refusal. A plain "no" is
// a warning — the user asked for something that does not apply, and nothing
// went wrong — while a check that could not run is reported as the error it is.
func (a *App) handleMergeWorkspaceRefused(msg messages.MergeWorkspaceRefused) tea.Cmd {
	if msg.Err != nil {
		return common.ReportError(
			errorContext(errorServiceWorkspace, "checking the merge precondition"),
			msg.Err,
			msg.Reason,
		)
	}
	return a.toast.ShowWarning(msg.Reason)
}

// checkedOutBranch resolves the branch checked out in repoPath.
func (a *App) checkedOutBranch(repoPath string) (string, error) {
	if a.checkedOutBranchFn != nil {
		return a.checkedOutBranchFn(repoPath)
	}
	return git.CheckedOutBranch(repoPath)
}

// localBaseBranch resolves the stored base ref to the local branch a merge
// would land on.
func (a *App) localBaseBranch(repoPath, base string) string {
	if a.localBaseBranchFn != nil {
		return a.localBaseBranchFn(repoPath, base)
	}
	return git.LocalBaseBranch(repoPath, base)
}

// handleShowMergeWorkspaceDialog presents the merge confirmation. The message
// spells out the exact git command so the user is agreeing to something
// specific, and the cursor defaults to "No" per the destructive-action
// convention.
func (a *App) handleShowMergeWorkspaceDialog(msg messages.ShowMergeWorkspaceDialog) {
	if msg.Workspace == nil {
		return
	}
	a.dialogWorkspace = msg.Workspace
	a.dialogMergeBase = msg.Base
	a.dialog = common.NewConfirmDialog(
		DialogMergeWorkspace,
		"Merge Workspace",
		fmt.Sprintf("Merge branch '%s' into '%s' in %s?\nRuns: git merge --no-ff -- %s",
			msg.Workspace.Branch, msg.Base, msg.Workspace.Repo, msg.Workspace.Branch),
	)
	// Repo hooks are neutralized on every amux git call, so a pre-merge hook the
	// user relies on will not fire. Say so rather than letting them find out.
	a.dialog.SetWarning("Repository git hooks are disabled for amux-run git commands.")
	a.presentDialog(a.dialog)
}

// mergeWorkspaceAsync performs the merge off the UI goroutine. It merges into
// whatever the primary checkout has checked out — the precondition in
// handleMergeWorkspace already established that this is the base branch.
func (a *App) mergeWorkspaceAsync(ws *data.Workspace, base string) tea.Cmd {
	if ws == nil {
		return nil
	}
	merge := a.mergeBranchFn
	if merge == nil {
		merge = git.MergeWorkspaceBranch
	}
	ctx := a.ctx
	repo := ws.Repo
	branch := ws.Branch
	return func() tea.Msg {
		return messages.WorkspaceMerged{
			Workspace: ws,
			Base:      base,
			Err:       merge(ctx, repo, branch),
		}
	}
}

// handleWorkspaceMerged reports the outcome. A conflict is routed to its own
// modal — it is a state the user has to act on, with an explicit Abort escape
// hatch — while every other failure is a plain error report.
func (a *App) handleWorkspaceMerged(msg messages.WorkspaceMerged) tea.Cmd {
	if msg.Workspace == nil {
		return nil
	}

	if msg.Err != nil {
		var conflict *git.MergeConflictError
		if errors.As(msg.Err, &conflict) {
			a.showMergeConflictDialog(msg.Workspace, conflict)
			return nil
		}
		return common.ReportError(
			errorContext(errorServiceWorkspace, "merging workspace branch"),
			msg.Err,
			"Merge failed: "+msg.Err.Error(),
		)
	}

	cmds := []tea.Cmd{a.toast.ShowSuccess("Merged into " + msg.Base)}

	// The merge advanced the base branch, so the workspace is now fully
	// contained in it; refresh the badge rather than leaving a stale "ahead".
	if a.sidebar != nil {
		cmds = append(cmds, a.sidebar.RefreshAheadBehind())
	}
	// What actually changed on disk is the *primary checkout*, not the
	// workspace's worktree — its HEAD moved and its tree gained the merged
	// files. The primary checkout is its own row on the dashboard, so its
	// cached status has to be dropped or it keeps showing pre-merge state.
	cmds = append(cmds, a.refreshPrimaryCheckoutStatus(msg.Workspace.Repo)...)

	return common.SafeBatch(cmds...)
}

// refreshPrimaryCheckoutStatus drops the cached git status for a project's
// primary checkout and requests a fresh one, after amux itself wrote to it.
func (a *App) refreshPrimaryCheckoutStatus(repo string) []tea.Cmd {
	if repo == "" {
		return nil
	}
	if a.gitStatus != nil {
		a.gitStatus.Invalidate(repo)
	}
	if a.dashboard != nil {
		a.dashboard.InvalidateStatus(repo)
	}
	return []tea.Cmd{a.requestGitStatusCached(repo, true)}
}

// showMergeConflictDialog lists the conflicted files and offers to abort. amux
// resolves nothing itself: the workspace already has a real shell, which is
// where conflict resolution belongs.
func (a *App) showMergeConflictDialog(ws *data.Workspace, conflict *git.MergeConflictError) {
	a.dialogWorkspace = ws
	a.dialog = common.NewConfirmDialog(
		DialogMergeConflict,
		"Merge Conflict",
		fmt.Sprintf("Merging '%s' stopped with conflicts:\n%s\n\nAbort the merge now?",
			conflict.Branch, formatConflictList(conflict.Files)),
	)
	// Default to "No" — leaving the merge in progress is the recoverable choice,
	// and the user may well want to resolve the conflicts in the terminal.
	a.dialog.SetWarning("Choosing No leaves the merge in progress to resolve in the terminal.")
	a.presentDialog(a.dialog)
}

// formatConflictList renders conflicted paths as an indented list, truncating
// past maxListedConflicts with an explicit count so nothing is hidden silently.
func formatConflictList(files []string) string {
	if len(files) == 0 {
		return "  (no files reported)"
	}
	shown := files
	var suffix string
	if len(files) > maxListedConflicts {
		shown = files[:maxListedConflicts]
		suffix = fmt.Sprintf("\n  ...and %d more", len(files)-maxListedConflicts)
	}
	return "  " + strings.Join(shown, "\n  ") + suffix
}

// abortWorkspaceMergeAsync abandons the in-progress merge off the UI goroutine.
func (a *App) abortWorkspaceMergeAsync(ws *data.Workspace) tea.Cmd {
	if ws == nil {
		return nil
	}
	abort := a.abortMergeFn
	if abort == nil {
		abort = git.AbortMerge
	}
	ctx := a.ctx
	repo := ws.Repo
	return func() tea.Msg {
		return messages.WorkspaceMergeAborted{Workspace: ws, Err: abort(ctx, repo)}
	}
}

// handleWorkspaceMergeAborted reports the abort outcome. A failed abort is
// serious — the repo is still mid-merge — so it is reported, not toasted away.
func (a *App) handleWorkspaceMergeAborted(msg messages.WorkspaceMergeAborted) tea.Cmd {
	if msg.Err != nil {
		return common.ReportError(
			errorContext(errorServiceWorkspace, "aborting merge"),
			msg.Err,
			"Abort failed; the repository is still mid-merge: "+msg.Err.Error(),
		)
	}

	cmds := []tea.Cmd{a.toast.ShowSuccess("Merge aborted")}
	// The abort rewound the primary checkout's tree and index, so its cached
	// status — which was captured mid-conflict — is stale.
	if msg.Workspace != nil {
		cmds = append(cmds, a.refreshPrimaryCheckoutStatus(msg.Workspace.Repo)...)
	}
	return common.SafeBatch(cmds...)
}
