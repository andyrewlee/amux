package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// TestHandleDialogResult_CommitWorkspaceInvokesCommitAll asserts a confirmed
// commit dialog routes to git.CommitAll (via the seam) with the workspace Root
// and the sanitized message, and yields a WorkspaceCommitted result.
func TestHandleDialogResult_CommitWorkspaceInvokesCommitAll(t *testing.T) {
	var gotRoot, gotMsg string
	called := false
	ws := &data.Workspace{Name: "feature", Root: "/tmp/ws", Branch: "feature"}
	app := &App{
		toast:           common.NewToastModel(),
		dialogWorkspace: ws,
		commitAllFn: func(_ context.Context, root, message string) error {
			called = true
			gotRoot = root
			gotMsg = message
			return nil
		},
	}

	cmd := app.handleDialogResult(common.DialogResult{
		ID:        DialogCommitWorkspace,
		Confirmed: true,
		Value:     "  wire commit-all  ",
	})
	if cmd == nil {
		t.Fatal("expected a commit command from a confirmed commit dialog")
	}
	msg := cmd()

	if !called {
		t.Fatal("expected the CommitAll seam to be invoked")
	}
	if gotRoot != "/tmp/ws" {
		t.Fatalf("commit ran in %q, want the workspace Root /tmp/ws", gotRoot)
	}
	if gotMsg != "wire commit-all" {
		t.Fatalf("commit message = %q, want sanitized (trimmed) %q", gotMsg, "wire commit-all")
	}
	committed, ok := msg.(messages.WorkspaceCommitted)
	if !ok {
		t.Fatalf("expected messages.WorkspaceCommitted, got %T", msg)
	}
	if committed.Workspace != ws || committed.Err != nil {
		t.Fatalf("unexpected WorkspaceCommitted: %+v", committed)
	}
}

// TestHandleDialogResult_CommitWorkspaceCanceledDoesNotCommit asserts an
// Esc/No dialog result never runs git.
func TestHandleDialogResult_CommitWorkspaceCanceledDoesNotCommit(t *testing.T) {
	called := false
	app := &App{
		toast:           common.NewToastModel(),
		dialogWorkspace: &data.Workspace{Name: "feature", Root: "/tmp/ws"},
		commitAllFn: func(context.Context, string, string) error {
			called = true
			return nil
		},
	}

	cmd := app.handleDialogResult(common.DialogResult{
		ID:        DialogCommitWorkspace,
		Confirmed: false,
		Value:     "ignored",
	})
	if cmd != nil {
		t.Fatal("expected no command from a canceled commit dialog")
	}
	if called {
		t.Fatal("canceled commit dialog must not invoke CommitAll")
	}
}

// TestHandleWorkspaceCommitted_ErrorReportsAndSkipsRefresh asserts a failed
// commit surfaces an error and does not silently succeed.
func TestHandleWorkspaceCommitted_ErrorReports(t *testing.T) {
	app := &App{toast: common.NewToastModel()}
	cmd := app.handleWorkspaceCommitted(messages.WorkspaceCommitted{
		Workspace: &data.Workspace{Root: "/tmp/ws"},
		Err:       errors.New("boom"),
	})
	if cmd == nil {
		t.Fatal("expected an error-reporting command")
	}
	// ReportError emits an Error message and an error Toast; assert the toast.
	found := false
	for _, m := range runCommandMessages(cmd) {
		if toast, ok := m.(messages.Toast); ok && toast.Level == messages.ToastError {
			found = true
			if !strings.Contains(toast.Message, "boom") {
				t.Fatalf("error toast missing cause: %q", toast.Message)
			}
		}
	}
	if !found {
		t.Fatal("expected an error toast from a failed commit")
	}
}

// TestHandleWorkspaceCommitted_SuccessToastsAndRefreshes asserts a successful
// commit shows a success toast and requests a status refresh.
func TestHandleWorkspaceCommitted_SuccessToastsAndRefreshes(t *testing.T) {
	// The refresh request needs a live service and an existing root: a
	// vanished root (or absent service) now yields no result message at all,
	// so a nil Status can never poison downstream caches.
	root := t.TempDir()
	stub := &fileWatcherGitStatusStub{}
	app := &App{toast: common.NewToastModel(), gitStatus: stub}
	cmd := app.handleWorkspaceCommitted(messages.WorkspaceCommitted{
		Workspace: &data.Workspace{Root: root},
	})
	if cmd == nil {
		t.Fatal("expected a success command")
	}
	if view := ansi.Strip(app.toast.View()); !strings.Contains(view, "Committed changes") {
		t.Fatalf("expected success toast, got %q", view)
	}
	sawStatus := false
	for _, m := range runCommandMessages(cmd) {
		if res, ok := m.(messages.GitStatusResult); ok && res.Root == root && res.Status != nil {
			sawStatus = true
		}
	}
	if !sawStatus {
		t.Fatal("expected a git-status refresh for the committed workspace")
	}
}
