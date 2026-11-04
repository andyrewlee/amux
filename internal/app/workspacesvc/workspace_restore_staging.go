package workspacesvc

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
)

// RestoreWorkspace's staging machine.
//
// Restore is a multi-stage write path (adopt-or-create → wait for git →
// clear flags) whose rollback obligation depends on PROVENANCE: a worktree
// this call created must be rolled back when a later stage fails, while a
// worktree adopted from disk (a crashed restore's leftover) must never be —
// it might be the user's only copy. The obligation used to live in the
// `created` bool captured by the rollback closure; here it is the machine's
// shape: the created lineage and the adopted lineage run as parallel state
// chains, and the failure edge into restoreRolledBack exists only from the
// created chain. Check proves it.
type restoreStagingState int

const (
	// restoreValidating: entry checks (flags, repo match, managed root,
	// branch) plus the already-live skip probe.
	restoreValidating restoreStagingState = iota
	// restoreAdopting: the root dir exists; worktree identity is being
	// verified before adoption.
	restoreAdopting
	// restoreAttaching: the root dir is absent; `worktree add` is in flight.
	restoreAttaching
	// restoreHeldAdopted: an existing, identity-verified worktree is in
	// hand — no rollback obligation.
	restoreHeldAdopted
	// restoreHeldCreated: this call created the worktree — a later failure
	// must remove it.
	restoreHeldCreated
	// restoreUnarchivingAdopted / restoreUnarchivingCreated: the .git path
	// appeared; the store flags are being cleared.
	restoreUnarchivingAdopted
	restoreUnarchivingCreated
	// restoreDone: flags cleared; the record is live again.
	restoreDone
	// restoreFailed: a stage failed with nothing this call owns to undo.
	restoreFailed
	// restoreRolledBack: a stage failed after this call created the
	// worktree, and the created worktree was removed (best effort).
	restoreRolledBack
)

func (s restoreStagingState) String() string {
	switch s {
	case restoreValidating:
		return "validating"
	case restoreAdopting:
		return "adopting"
	case restoreAttaching:
		return "attaching"
	case restoreHeldAdopted:
		return "held-adopted"
	case restoreHeldCreated:
		return "held-created"
	case restoreUnarchivingAdopted:
		return "unarchiving-adopted"
	case restoreUnarchivingCreated:
		return "unarchiving-created"
	case restoreDone:
		return "done"
	case restoreRolledBack:
		return "rolled-back"
	default:
		return "failed"
	}
}

type restoreStagingEvent int

const (
	// evtRestoreDirPresent: the root dir stat succeeded (adoption branch).
	evtRestoreDirPresent restoreStagingEvent = iota
	// evtRestoreDirAbsent: the root dir is proven absent (creation branch).
	evtRestoreDirAbsent
	// evtRestoreAdoptVerified: the dir is a worktree of this repo on this
	// branch — adopt it.
	evtRestoreAdoptVerified
	// evtRestoreAttached: `worktree add` completed — this call created the
	// worktree.
	evtRestoreAttached
	// evtRestoreGitReady: the .git path exists.
	evtRestoreGitReady
	// evtRestoreFlagsCleared: the store update cleared the shelf flags.
	evtRestoreFlagsCleared
	// evtRestoreStageFailed: a stage returned an error.
	evtRestoreStageFailed
)

func (e restoreStagingEvent) String() string {
	switch e {
	case evtRestoreDirPresent:
		return "dir-present"
	case evtRestoreDirAbsent:
		return "dir-absent"
	case evtRestoreAdoptVerified:
		return "adopt-verified"
	case evtRestoreAttached:
		return "attached"
	case evtRestoreGitReady:
		return "git-ready"
	case evtRestoreFlagsCleared:
		return "flags-cleared"
	default:
		return "stage-failed"
	}
}

// restoreStagingMachine is the restore staging protocol. The two lineages
// never cross: once the dir-probe picks adopt or create, every later stage
// (git wait, flag clear, failure handling) stays in that lineage, so the
// rollback obligation cannot be lost or gained mid-restore.
var restoreStagingMachine = fsm.MustCompile(fsm.Machine[restoreStagingState, restoreStagingEvent]{
	Name:    "workspace-restore-staging",
	Initial: restoreValidating,
	Steps: []fsm.Step[restoreStagingState, restoreStagingEvent]{
		// The dir probe forks the two lineages.
		{From: restoreValidating, Event: evtRestoreDirPresent, To: restoreAdopting},
		{From: restoreValidating, Event: evtRestoreDirAbsent, To: restoreAttaching},
		{From: restoreValidating, Event: evtRestoreStageFailed, To: restoreFailed},
		// Adoption: identity checks may still reject (foreign worktree) —
		// with nothing of ours to undo.
		{From: restoreAdopting, Event: evtRestoreAdoptVerified, To: restoreHeldAdopted},
		{From: restoreAdopting, Event: evtRestoreStageFailed, To: restoreFailed},
		// Creation: a failed attach leaves nothing (AttachWorkspace rolls
		// back its own partial add); a successful one owes a rollback.
		{From: restoreAttaching, Event: evtRestoreAttached, To: restoreHeldCreated},
		{From: restoreAttaching, Event: evtRestoreStageFailed, To: restoreFailed},
		// Git wait then flag clear, per lineage.
		{From: restoreHeldAdopted, Event: evtRestoreGitReady, To: restoreUnarchivingAdopted},
		{From: restoreHeldCreated, Event: evtRestoreGitReady, To: restoreUnarchivingCreated},
		{From: restoreUnarchivingAdopted, Event: evtRestoreFlagsCleared, To: restoreDone},
		{From: restoreUnarchivingCreated, Event: evtRestoreFlagsCleared, To: restoreDone},
		// Failures: only the created lineage rolls back.
		{From: restoreHeldAdopted, Event: evtRestoreStageFailed, To: restoreFailed},
		{From: restoreUnarchivingAdopted, Event: evtRestoreStageFailed, To: restoreFailed},
		{From: restoreHeldCreated, Event: evtRestoreStageFailed, To: restoreRolledBack},
		{From: restoreUnarchivingCreated, Event: evtRestoreStageFailed, To: restoreRolledBack},
	},
	Terminals: []restoreStagingState{restoreDone, restoreFailed, restoreRolledBack},
})

// restoreStagingStep advances the cursor, returning the new stage. A
// rejected move keeps the current stage — the call site treats an
// out-of-order observation as a no-op rather than a crash, matching the
// fsm package's fail-closed convention.
func restoreStagingStep(from restoreStagingState, event restoreStagingEvent) restoreStagingState {
	if next, ok := restoreStagingMachine.Next(from, event); ok {
		return next
	}
	return from
}

// restoreStagingOwesRollback reports whether a failure at this stage must
// roll back the worktree — exactly the created lineage.
func restoreStagingOwesRollback(s restoreStagingState) bool {
	return s == restoreHeldCreated || s == restoreUnarchivingCreated
}

// RestoreStagingCheck formally verifies the staging protocol: the rollback
// terminal is reachable only via a stage failure from the created lineage
// (an adopted worktree can never be rolled back), adoption is reachable
// only through a present dir plus a verified identity, creation only
// through a proven-absent dir, and every in-flight stage can still reach
// both restoreDone and a terminal (no wedged restores). Runs in
// workspacesvc's tests.
func RestoreStagingCheck() error {
	if err := restoreStagingMachine.Check(fsm.Checker[restoreStagingState, restoreStagingEvent]{
		Alphabet: []restoreStagingEvent{
			evtRestoreDirPresent, evtRestoreDirAbsent, evtRestoreAdoptVerified,
			evtRestoreAttached, evtRestoreGitReady, evtRestoreFlagsCleared,
			evtRestoreStageFailed,
		},
		AllDeclaredReachable: true,
		Edge: func(from restoreStagingState, event restoreStagingEvent, to restoreStagingState) error {
			if to == restoreRolledBack && (!restoreStagingOwesRollback(from) || event != evtRestoreStageFailed) {
				return fmt.Errorf("rollback from %v via %v: only a stage failure on the created lineage may roll back", from, event)
			}
			if to == restoreHeldCreated && event != evtRestoreAttached {
				return fmt.Errorf("created-worktree state entered via %v: only a completed `worktree add` creates the rollback obligation", event)
			}
			if to == restoreHeldAdopted && event != evtRestoreAdoptVerified {
				return errors.New("adoption requires a present dir with a verified identity")
			}
			if from == restoreValidating && event == evtRestoreAdoptVerified {
				return errors.New("adoption must go through the dir-present branch, never straight from validation")
			}
			return nil
		},
	}); err != nil {
		return err
	}
	// Every in-flight stage terminates, and completion stays possible from
	// every pre-terminal stage (a crashed restore is always retryable).
	alphabet := []restoreStagingEvent{
		evtRestoreDirPresent, evtRestoreDirAbsent, evtRestoreAdoptVerified,
		evtRestoreAttached, evtRestoreGitReady, evtRestoreFlagsCleared,
		evtRestoreStageFailed,
	}
	for _, from := range []restoreStagingState{
		restoreValidating, restoreAdopting, restoreAttaching,
		restoreHeldAdopted, restoreHeldCreated,
		restoreUnarchivingAdopted, restoreUnarchivingCreated,
	} {
		reachable := restoreStagingMachine.ReachableFrom(from, alphabet)
		done := false
		for _, st := range reachable {
			if st == restoreDone {
				done = true
				break
			}
		}
		if !done {
			return fmt.Errorf("stage %v cannot reach done: an in-flight restore must stay completable", from)
		}
	}
	return nil
}
