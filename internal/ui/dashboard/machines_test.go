package dashboard

import (
	"slices"
	"testing"
)

// TestAttentionMachineCheck formally verifies the per-workspace badge
// machine: latch lifetimes, the Working-survival property of the attention
// latch, the watched Working→Done latch, and the ack suppression.
func TestAttentionMachineCheck(t *testing.T) {
	t.Parallel()
	if err := attentionMachineCheck(); err != nil {
		t.Fatal(err)
	}
}

// TestPendingSelectMachineCheck formally verifies the pending-selection
// debounce: both states reachable, every Armed exit lands Idle, arming is
// the only way in.
func TestPendingSelectMachineCheck(t *testing.T) {
	t.Parallel()
	if err := pendingSelectMachineCheck(); err != nil {
		t.Fatal(err)
	}
	// No-leak claim: from Armed, every reachable state can still reach Idle.
	alphabet := []pendingSelectEvent{
		pendingSelectArm, pendingSelectApply, pendingSelectExpire, pendingSelectClear,
	}
	for _, st := range pendingSelectMachine.ReachableFrom(pendingSelectArmed, alphabet) {
		if !slices.Contains(pendingSelectMachine.ReachableFrom(st, alphabet), pendingSelectIdle) {
			t.Fatalf("state %v cannot return to Idle — a selection can strand", st)
		}
	}
}

// TestWorkspaceOpMachineCheck formally verifies the per-root lifecycle
// machine: every finish event returns to Idle, and each start lands its own
// state — no path latches a root Creating or Busy forever.
func TestWorkspaceOpMachineCheck(t *testing.T) {
	t.Parallel()
	if err := workspaceOpMachineCheck(); err != nil {
		t.Fatal(err)
	}
	alphabet := []workspaceOpEvent{opEventCreate, opEventCreated, opEventBegin, opEventEnd}
	for _, st := range []workspaceOpState{opCreating, opBusy} {
		if !slices.Contains(workspaceOpMachine.ReachableFrom(st, alphabet), 0) {
			t.Fatalf("state %v cannot return to Idle — an op can leak latched", st)
		}
	}
}

// TestWorkspaceOpRejectedClearKeepsPayload pins the fail-closed no-op: a
// creating-finished event on a Busy root (and vice versa) must not wipe the
// held verb or payload.
func TestWorkspaceOpRejectedClearKeepsPayload(t *testing.T) {
	t.Parallel()
	m := New()
	m.SetWorkspaceBusy("/r", WorkspaceOpShelve, true)
	m.stepWorkspaceOp("/r", opEventCreated, nil, "") // wrong finish: rejected
	if op, busy := m.busyOp("/r"); !busy || op != WorkspaceOpShelve {
		t.Fatalf("rejected clear wiped the held verb: busy=%v op=%q", busy, op)
	}
	m.SetWorkspaceBusy("/r", WorkspaceOpShelve, false)
	if len(m.workspaceOps) != 0 {
		t.Fatal("op did not return to Idle on its own finish event")
	}
}
