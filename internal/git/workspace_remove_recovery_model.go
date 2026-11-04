package git

import (
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
)

// The removal protocol as a machine — the verified shape of the decisions
// in workspace_remove_recovery.go. States are the positions RemoveWorkspace
// moves through; the destructive positions are the persistAndResume
// recovery modes.
type removalState uint8

const (
	removeProbeMarker removalState = iota
	removeResumePending
	removeProbeRegistration
	removeRegistered
	removeUnregistered
	removeOwnershipLost
	removeDirAbsent
	removeDirPresent
	removeSentinelUnregistered
	removeLegacyManaged
	removeForceFailedContext
	removeForceFailedRemnants
	removeDone
	removeFailed
)

func (s removalState) String() string {
	switch s {
	case removeProbeMarker:
		return "probe-marker"
	case removeResumePending:
		return "resume-pending"
	case removeProbeRegistration:
		return "probe-registration"
	case removeRegistered:
		return "registered"
	case removeUnregistered:
		return "unregistered"
	case removeOwnershipLost:
		return "ownership-lost"
	case removeDirAbsent:
		return "dir-absent"
	case removeDirPresent:
		return "dir-present"
	case removeSentinelUnregistered:
		return "sentinel-unregistered"
	case removeLegacyManaged:
		return "legacy-managed"
	case removeForceFailedContext:
		return "force-failed-context"
	case removeForceFailedRemnants:
		return "force-failed-remnants"
	case removeDone:
		return "done"
	default:
		return "failed"
	}
}

type removalEvent uint8

const (
	evtMarkerFound removalEvent = iota
	evtMarkerAbsent
	evtGitListRegistered
	evtGitListUnregistered
	evtGitListFailed
	evtDirAbsentSentinel
	evtDirPresentSentinel
	evtGitFileAbsent
	evtGitFilePresent
	evtRetryMarkerFound
	evtRetryMarkerAbsent
	evtLegacyManagedRoot
	evtForeignRoot
	evtRemovalConfirmedGone
	evtForceSucceeded
	evtForceFailedContext
	evtForceFailedRemnants
	evtForceFailedHard
	evtCleanupSucceeded
	evtCleanupFailed
)

func (e removalEvent) String() string {
	switch e {
	case evtMarkerFound:
		return "marker-found"
	case evtMarkerAbsent:
		return "marker-absent"
	case evtGitListRegistered:
		return "git-list-registered"
	case evtGitListUnregistered:
		return "git-list-unregistered"
	case evtGitListFailed:
		return "git-list-failed"
	case evtDirAbsentSentinel:
		return "dir-absent-sentinel"
	case evtDirPresentSentinel:
		return "dir-present-sentinel"
	case evtGitFileAbsent:
		return "git-file-absent"
	case evtGitFilePresent:
		return "git-file-present"
	case evtRetryMarkerFound:
		return "retry-marker-found"
	case evtRetryMarkerAbsent:
		return "retry-marker-absent"
	case evtLegacyManagedRoot:
		return "legacy-managed-root"
	case evtForeignRoot:
		return "foreign-root"
	case evtRemovalConfirmedGone:
		return "removal-confirmed-gone"
	case evtForceSucceeded:
		return "force-succeeded"
	case evtForceFailedContext:
		return "force-failed-context"
	case evtForceFailedRemnants:
		return "force-failed-remnants"
	case evtForceFailedHard:
		return "force-failed-hard"
	case evtCleanupSucceeded:
		return "cleanup-succeeded"
	default:
		return "cleanup-failed"
	}
}

var removalAlphabet = []removalEvent{
	evtMarkerFound, evtMarkerAbsent,
	evtGitListRegistered, evtGitListUnregistered, evtGitListFailed,
	evtDirAbsentSentinel, evtDirPresentSentinel,
	evtGitFileAbsent, evtGitFilePresent,
	evtRetryMarkerFound, evtRetryMarkerAbsent,
	evtLegacyManagedRoot, evtForeignRoot,
	evtRemovalConfirmedGone,
	evtForceSucceeded, evtForceFailedContext, evtForceFailedRemnants, evtForceFailedHard,
	evtCleanupSucceeded, evtCleanupFailed,
}

// removalMachine is the remove/recovery protocol. The safety-critical shape:
// each destructive state (legacy-managed, force-failed-context,
// force-failed-remnants) has exactly one incoming edge, and the predecessor
// chain to any of them includes proof that no healthy repository owns the
// path — git's own list said unregistered, the `.git` file is proven
// absent, or git itself broke.
var removalMachine = fsm.MustCompile(fsm.Machine[removalState, removalEvent]{
	Name:    "worktree-removal-recovery",
	Initial: removeProbeMarker,
	Steps: []fsm.Step[removalState, removalEvent]{
		// Entry: the durable cleanup marker is the most specific intent.
		{From: removeProbeMarker, Event: evtMarkerFound, To: removeResumePending},
		{From: removeProbeMarker, Event: evtMarkerAbsent, To: removeProbeRegistration},
		{From: removeResumePending, Event: evtCleanupSucceeded, To: removeDone},
		{From: removeResumePending, Event: evtCleanupFailed, To: removeFailed},
		// Registration decides between git-driven removal and sentinels.
		{From: removeProbeRegistration, Event: evtGitListRegistered, To: removeRegistered},
		{From: removeProbeRegistration, Event: evtGitListUnregistered, To: removeUnregistered},
		{From: removeProbeRegistration, Event: evtGitListFailed, To: removeOwnershipLost},
		{From: removeUnregistered, Event: evtCleanupSucceeded, To: removeDone},
		{From: removeUnregistered, Event: evtCleanupFailed, To: removeFailed},
		// Git unavailable: filesystem sentinels decide, fail closed.
		{From: removeOwnershipLost, Event: evtDirAbsentSentinel, To: removeDirAbsent},
		{From: removeOwnershipLost, Event: evtDirPresentSentinel, To: removeDirPresent},
		{From: removeDirAbsent, Event: evtRemovalConfirmedGone, To: removeDone},
		{From: removeDirPresent, Event: evtGitFilePresent, To: removeFailed},
		{From: removeDirPresent, Event: evtGitFileAbsent, To: removeSentinelUnregistered},
		{From: removeSentinelUnregistered, Event: evtRetryMarkerFound, To: removeUnregistered},
		{From: removeSentinelUnregistered, Event: evtRetryMarkerAbsent, To: removeSentinelUnregistered},
		{From: removeSentinelUnregistered, Event: evtLegacyManagedRoot, To: removeLegacyManaged},
		{From: removeSentinelUnregistered, Event: evtForeignRoot, To: removeUnregistered},
		// Destructive legacy recovery (amux's own leftover path).
		{From: removeLegacyManaged, Event: evtCleanupSucceeded, To: removeDone},
		{From: removeLegacyManaged, Event: evtCleanupFailed, To: removeFailed},
		// Registered: force remove, then re-classify on failure.
		{From: removeRegistered, Event: evtForceSucceeded, To: removeDone},
		{From: removeRegistered, Event: evtForceFailedContext, To: removeForceFailedContext},
		{From: removeRegistered, Event: evtForceFailedRemnants, To: removeForceFailedRemnants},
		{From: removeRegistered, Event: evtForceFailedHard, To: removeFailed},
		{From: removeForceFailedContext, Event: evtCleanupSucceeded, To: removeDone},
		{From: removeForceFailedContext, Event: evtCleanupFailed, To: removeFailed},
		{From: removeForceFailedRemnants, Event: evtCleanupSucceeded, To: removeDone},
		{From: removeForceFailedRemnants, Event: evtCleanupFailed, To: removeFailed},
	},
	Terminals: []removalState{removeDone, removeFailed},
})

// removalStateDestructive reports the states whose action can delete the
// workspace directory (the persistAndResumeWorkspaceCleanup modes).
func removalStateDestructive(s removalState) bool {
	switch s {
	case removeLegacyManaged, removeForceFailedContext, removeForceFailedRemnants:
		return true
	}
	return false
}

// WorkspaceRemovalCheck formally verifies the removal protocol: the machine
// is well-formed (every state reachable, terminals without outgoing edges,
// done reachable from every in-flight position), every destructive position
// has exactly one legal entry event, no destructive position is reachable
// with healthy git and proven registration (or from a proven-gone dir), and
// the two decision functions are exact over their whole input spaces. Runs
// in git's tests.
func WorkspaceRemovalCheck() error {
	if err := removalMachine.Check(fsm.Checker[removalState, removalEvent]{
		Alphabet:             removalAlphabet,
		AllDeclaredReachable: true,
		Edge:                 removalDestructiveEntryEdge,
	}); err != nil {
		return err
	}
	if err := removalAlwaysCompletable(); err != nil {
		return err
	}
	if err := removalProbesCannotDestroy(); err != nil {
		return err
	}
	if err := removalClassifySweep(); err != nil {
		return err
	}
	return removalForceSweep()
}

// removalDestructiveEntryEdge pins the single-authorizer rule: each
// destructive position is entered by exactly its own authorizing event.
func removalDestructiveEntryEdge(_ removalState, event removalEvent, to removalState) error {
	if to == removeLegacyManaged && event != evtLegacyManagedRoot {
		return fmt.Errorf("legacy recovery entered via %v: only a legacy managed root may authorize it", event)
	}
	if to == removeForceFailedContext && event != evtForceFailedContext {
		return fmt.Errorf("context recovery entered via %v: only a git context failure may authorize it", event)
	}
	if to == removeForceFailedRemnants && event != evtForceFailedRemnants {
		return fmt.Errorf("remnant recovery entered via %v: only an unregistered remnant may authorize it", event)
	}
	return nil
}

// removalAlwaysCompletable proves every in-flight position can still
// complete: a removal must never dead-end.
func removalAlwaysCompletable() error {
	for _, from := range []removalState{
		removeProbeMarker, removeResumePending, removeProbeRegistration,
		removeRegistered, removeUnregistered, removeOwnershipLost,
		removeDirAbsent, removeDirPresent, removeSentinelUnregistered,
		removeLegacyManaged, removeForceFailedContext,
		removeForceFailedRemnants,
	} {
		done := false
		for _, st := range removalMachine.ReachableFrom(from, removalAlphabet) {
			if st == removeDone {
				done = true
				break
			}
		}
		if !done {
			return fmt.Errorf("position %v cannot reach done: a removal must always be completable", from)
		}
	}
	return nil
}

// removalProbesCannotDestroy proves no destructive position is reachable
// from a healthy starting point: with git healthy and the worktree
// registered, probe events alone (no force failure) never authorize
// destruction, and a proven-gone dir never turns destructive either.
func removalProbesCannotDestroy() error {
	probeOnly := make([]removalEvent, 0, len(removalAlphabet))
	for _, e := range removalAlphabet {
		if e != evtForceFailedContext && e != evtForceFailedRemnants {
			probeOnly = append(probeOnly, e)
		}
	}
	for _, st := range removalMachine.ReachableFrom(removeRegistered, probeOnly) {
		if removalStateDestructive(st) {
			return fmt.Errorf("state %v reachable from registered without a force failure: destruction requires broken git or broken registration", st)
		}
	}
	for _, st := range removalMachine.ReachableFrom(removeDirAbsent, removalAlphabet) {
		if removalStateDestructive(st) {
			return fmt.Errorf("state %v reachable from dir-absent: a gone dir needs no destructive recovery", st)
		}
	}
	return nil
}

// removalClassifySweep is the stage-one decision sweep, stated as the
// original decision tree's independent properties rather than a
// re-derivation.
func removalClassifySweep() error {
	for _, marker := range []bool{false, true} {
		for _, ownership := range []worktreeOwnership{ownershipRegistered, ownershipUnregistered, ownershipUnknown} {
			for _, dir := range []pathSentinel{sentinelPresent, sentinelAbsent, sentinelError, sentinelUnknown} {
				for _, gitFile := range []pathSentinel{sentinelPresent, sentinelAbsent, sentinelError, sentinelUnknown} {
					for _, retry := range []bool{false, true} {
						for _, legacy := range []bool{false, true} {
							if err := removalClassifyProbes(marker, ownership, dir, gitFile, retry, legacy); err != nil {
								return err
							}
						}
					}
				}
			}
		}
	}
	return nil
}

// removalClassifyProbes checks one probe combination against the three
// stage-one properties.
func removalClassifyProbes(marker bool, ownership worktreeOwnership, dir, gitFile pathSentinel, retry, legacy bool) error {
	p := removalProbes{ownership: ownership, dir: dir, gitFile: gitFile, retryMarker: retry, legacyManaged: legacy}
	got := classifyWorkspaceRemoval(marker, p)
	// The marker is the most specific intent on disk.
	if marker && got != removalClassResumeMarker {
		return fmt.Errorf("probes %+v with pending marker = %v: a pending marker always resumes", p, got)
	}
	// Destructive entry recovery happens for exactly one combination: git
	// broken, dir present, .git proven absent, no retry marker, legacy root.
	legacyCombo := !marker && ownership == ownershipUnknown &&
		dir == sentinelPresent && gitFile == sentinelAbsent && !retry && legacy
	if (got == removalClassLegacyRecovery) != legacyCombo {
		return fmt.Errorf("probes %+v = %v: destructive recovery must be authorized by exactly the legacy-leftover combination", p, got)
	}
	// A present or errored .git under unknown ownership never authorizes
	// anything but failure.
	if !marker && ownership == ownershipUnknown && dir == sentinelPresent &&
		gitFile != sentinelAbsent && got != removalClassFail {
		return fmt.Errorf("probes %+v = %v: an unproven .git sentinel must fail closed", p, got)
	}
	return nil
}

// removalForceSweep is the stage-two sweep: destruction requires a git
// context failure, or a proven-absent .git on a safe path.
func removalForceSweep() error {
	for _, gitContext := range []bool{false, true} {
		for _, gitFile := range []pathSentinel{sentinelPresent, sentinelAbsent, sentinelError, sentinelUnknown} {
			for _, safe := range []bool{false, true} {
				got := classifyForceFailure(gitContext, gitFile, safe)
				if want := gitContext || (gitFile == sentinelAbsent && safe); got.destructive() != want {
					return fmt.Errorf("classifyForceFailure(gitContext=%v gitFile=%v safe=%v) = %v: destruction must require broken git or a proven-absent .git on a safe path",
						gitContext, gitFile, safe, got)
				}
			}
		}
	}
	return nil
}
