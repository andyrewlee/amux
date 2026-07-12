package git

// This file is a compile-checked API skeleton for the design spike
// "Minimal safe git write-back surface (commit / merge from the UI)", which
// lives with the maintainer's planning notes under plans/design/. It declares
// the proposed v1 write-back signatures as unexported stubs so their shapes are
// validated by the Go compiler, without shipping any wired production code:
// nothing here is exported, keybound, or invoked. Delete or promote these once
// the feature is actually built.

import (
	"context"
	"errors"
	"testing"
)

// errWritebackDesignNotImplemented is the sentinel every design stub returns.
// It exists only so the skeleton has observable, testable behavior; the real
// implementation replaces these stubs entirely.
var errWritebackDesignNotImplemented = errors.New("git write-back: not implemented (design skeleton)")

// commitAllDesign is the proposed CommitAll: stage everything in workspaceRoot
// (git add -A) and commit it with message (git commit -m <message>). See
// Action 1 in the design doc.
func commitAllDesign(ctx context.Context, workspaceRoot, message string) error {
	_, _, _ = ctx, workspaceRoot, message
	return errWritebackDesignNotImplemented
}

// mergeWorkspaceBranchDesign is the proposed MergeWorkspaceBranch: merge branch
// into the branch already checked out in repoPath (git merge --no-ff -- <branch>).
// It never checks the base out implicitly. See Action 2 in the design doc.
func mergeWorkspaceBranchDesign(ctx context.Context, repoPath, branch string) error {
	_, _, _ = ctx, repoPath, branch
	return errWritebackDesignNotImplemented
}

// abortMergeDesign is the proposed AbortMerge: abort an in-progress merge in
// repoPath (git merge --abort). See the conflict path in Action 2.
func abortMergeDesign(ctx context.Context, repoPath string) error {
	_, _ = ctx, repoPath
	return errWritebackDesignNotImplemented
}

// Compile-time signature guards: if the proposed API shape drifts, the build
// breaks here before any test runs.
var (
	_ func(context.Context, string, string) error = commitAllDesign
	_ func(context.Context, string, string) error = mergeWorkspaceBranchDesign
	_ func(context.Context, string) error         = abortMergeDesign
)

// TestWritebackDesignAPISkeleton asserts the proposed stubs exist with the
// intended signatures and return the not-implemented sentinel, then skips: this
// is a design skeleton, not a behavioral test, and nothing is wired yet.
func TestWritebackDesignAPISkeleton(t *testing.T) {
	ctx := context.Background()

	if err := commitAllDesign(ctx, "/ws", "msg"); !errors.Is(err, errWritebackDesignNotImplemented) {
		t.Fatalf("commitAllDesign: got %v, want not-implemented sentinel", err)
	}
	if err := mergeWorkspaceBranchDesign(ctx, "/repo", "branch"); !errors.Is(err, errWritebackDesignNotImplemented) {
		t.Fatalf("mergeWorkspaceBranchDesign: got %v, want not-implemented sentinel", err)
	}
	if err := abortMergeDesign(ctx, "/repo"); !errors.Is(err, errWritebackDesignNotImplemented) {
		t.Fatalf("abortMergeDesign: got %v, want not-implemented sentinel", err)
	}

	t.Skip("design skeleton: signatures are compile-checked and stubs return not-implemented; no behavior is wired yet")
}
