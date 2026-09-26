package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// TestMergeConfirmDialog_RechecksDestinationAtExecution is the plan-035
// regression: the primary checkout can move while the confirm dialog is
// open, and the command must refuse rather than merge into the new HEAD.
func TestMergeConfirmDialog_RechecksDestinationAtExecution(t *testing.T) {
	ws := mergeWorkspace()
	app := newMergeApp("main")
	mergeCalls := 0
	app.mergeBranchFn = func(context.Context, string, string) error {
		mergeCalls++
		return nil
	}

	app.handleShowMergeWorkspaceDialog(messages.ShowMergeWorkspaceDialog{Workspace: ws, Base: "main"})
	// The checkout moved between preflight and confirmation.
	app.checkedOutBranchFn = func(string) (string, error) {
		return "hotfix", nil
	}
	cmd := app.handleDialogResult(common.DialogResult{ID: DialogMergeWorkspace, Confirmed: true}, app.dlg)
	if cmd == nil {
		t.Fatal("confirming the merge dialog produced no command")
	}
	msg := cmd()
	refused, ok := msg.(messages.MergeWorkspaceRefused)
	if !ok {
		t.Fatalf("moved destination produced %T, want MergeWorkspaceRefused", msg)
	}
	if mergeCalls != 0 {
		t.Fatal("merge ran even though the approved destination moved")
	}
	if !strings.Contains(refused.Reason, "hotfix") || !strings.Contains(refused.Reason, "main") {
		t.Fatalf("refusal %q should name both the moved-to branch and the approved base", refused.Reason)
	}
}

// TestMergeConfirmDialog_ExecutionRecheckFailures covers the other ways the
// re-verification can fail: detached HEAD (a query error) and an empty
// approved base. Both refuse without touching the merge seam.
func TestMergeConfirmDialog_ExecutionRecheckFailures(t *testing.T) {
	t.Run("detached head", func(t *testing.T) {
		app := newMergeApp("main")
		app.mergeBranchFn = func(context.Context, string, string) error {
			t.Fatal("merge ran on an unverifiable checkout")
			return nil
		}
		app.handleShowMergeWorkspaceDialog(messages.ShowMergeWorkspaceDialog{Workspace: mergeWorkspace(), Base: "main"})
		app.checkedOutBranchFn = func(string) (string, error) {
			return "", errors.New("fatal: ref HEAD is not a symbolic ref")
		}
		cmd := app.handleDialogResult(common.DialogResult{ID: DialogMergeWorkspace, Confirmed: true}, app.dlg)
		refused, ok := cmd().(messages.MergeWorkspaceRefused)
		if !ok {
			t.Fatalf("detached HEAD at execution produced %T, want MergeWorkspaceRefused", cmd())
		}
		if refused.Err == nil {
			t.Fatal("the execution-time refusal dropped the underlying git error")
		}
	})
	t.Run("empty approved base", func(t *testing.T) {
		app := newMergeApp("main")
		app.mergeBranchFn = func(context.Context, string, string) error {
			t.Fatal("merge ran with no approved base")
			return nil
		}
		cmd := app.mergeWorkspaceAsync(mergeWorkspace(), "  ")
		if _, ok := cmd().(messages.MergeWorkspaceRefused); !ok {
			t.Fatalf("empty approved base produced %T, want MergeWorkspaceRefused", cmd())
		}
	})
}
