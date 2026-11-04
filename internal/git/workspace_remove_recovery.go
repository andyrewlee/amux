package git

import (
	"os"
	"path/filepath"
)

// RemoveWorkspace recovery-path selection.
//
// Removing a workspace means interpreting a pile of filesystem sentinels —
// a durable cleanup marker, git's worktree list, the workspace dir, its
// `.git` file, a retry-metadata marker, the legacy managed-root layout —
// and choosing among recovery actions, three of which are DESTRUCTIVE
// (persistAndResumeWorkspaceCleanup can delete the directory). The same
// combo used to be reinterpreted inline in three places (RemoveWorkspace,
// prepareWorkspacePathForCreate via the retry-metadata guard, and the
// service layer's stale-remove triage), so this file makes the selection an
// explicit, table-checked decision: the sentinel combinations become
// enumerated states, the destructive states are reachable only through
// proven-foreign or proven-broken ownership, and RemoveWorkspace switches
// on the class.

// probeUnprovenOwnershipSentinels gathers the filesystem facts the
// classifier needs when git's worktree list could not prove ownership:
// dir presence, and — only for a present dir — the `.git` sentinel, the
// retry marker, and (marker-less) the legacy-managed-root check. Probe
// errors propagate verbatim to the caller.
func probeUnprovenOwnershipSentinels(repoPath, workspacePath string) (removalProbes, error) {
	probes := removalProbes{
		ownership: ownershipUnknown,
		dir:       sentinelUnknown,
		gitFile:   sentinelUnknown,
	}
	dirSentinel, statErr := probeSentinel(workspacePath)
	if statErr != nil {
		return probes, statErr
	}
	probes.dir = dirSentinel
	if dirSentinel != sentinelPresent {
		return probes, nil
	}
	gitSentinel, gitStatErr := probeSentinel(filepath.Join(workspacePath, ".git"))
	if gitStatErr != nil {
		return probes, gitStatErr
	}
	probes.gitFile = gitSentinel
	if gitSentinel != sentinelAbsent {
		return probes, nil
	}
	_, retryMarked, retryErr := readWorkspaceCleanupRetryMetadata(workspacePath)
	if retryErr != nil {
		return probes, retryErr
	}
	probes.retryMarker = retryMarked
	if !retryMarked {
		probes.legacyManaged = isLegacyManagedWorkspacePathForRepo(repoPath, workspacePath)
	}
	return probes, nil
}

// pathSentinel is one filesystem probe's outcome. Only sentinelAbsent is
// proof of absence; an I/O or permission error is NOT (fail closed).
type pathSentinel uint8

const (
	sentinelPresent pathSentinel = iota
	sentinelAbsent
	sentinelError
	sentinelUnknown // not probed on this path
)

func (s pathSentinel) String() string {
	switch s {
	case sentinelAbsent:
		return "absent"
	case sentinelError:
		return "error"
	case sentinelUnknown:
		return "unknown"
	default:
		return "present"
	}
}

// probeSentinel stats path and classifies it. The error return preserves the
// original os.Stat error for propagation by the caller; the classifier's
// exhaustive sweep is the only consumer of sentinelError.
func probeSentinel(path string) (pathSentinel, error) {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return sentinelPresent, nil
	case os.IsNotExist(err):
		return sentinelAbsent, nil
	default:
		return sentinelError, err
	}
}

// worktreeOwnership is what git's worktree list proved (or failed to prove)
// about the path.
type worktreeOwnership uint8

const (
	ownershipRegistered   worktreeOwnership = iota // list ran; path is in it
	ownershipUnregistered                          // list ran; path is not in it
	ownershipUnknown                               // list failed — git unavailable
)

func (o worktreeOwnership) String() string {
	switch o {
	case ownershipRegistered:
		return "registered"
	case ownershipUnregistered:
		return "unregistered"
	default:
		return "unknown"
	}
}

// removalProbes is the typed observation RemoveWorkspace reduces to before
// choosing a recovery path. Fields are meaningful only for the ownerships
// that actually probed them — mirroring which probes the original decision
// tree ran (dir/.git/retry/legacy only under unknown ownership).
type removalProbes struct {
	ownership     worktreeOwnership
	dir           pathSentinel
	gitFile       pathSentinel
	retryMarker   bool
	legacyManaged bool
}

// removalClass is the chosen recovery path. Three classes run destructive
// filesystem recovery (persistAndResumeWorkspaceCleanup): LegacyRecovery
// here, ContextRecovery and RemnantRecovery in the force-failure stage.
type removalClass uint8

const (
	// removalClassResumeMarker: a durable cleanup marker exists — resume it,
	// whatever else the sentinels say (the marker is the most specific
	// intent on disk).
	removalClassResumeMarker removalClass = iota
	// removalClassForceRemove: git knows the worktree — one
	// `worktree remove --force`, with failures re-classified afterward.
	removalClassForceRemove
	// removalClassCleanUnregistered: git (or the sentinels, once git is
	// broken) says no repository owns the path — cleanupOrValidate.
	removalClassCleanUnregistered
	// removalClassSucceedAbsent: the dir is proven gone — nothing to remove.
	removalClassSucceedAbsent
	// removalClassLegacyRecovery (destructive): git is unavailable, the dir
	// exists without a `.git` file, no retry marker, and the path sits under
	// a legacy managed root — amux's own leftover, so resume its cleanup
	// with unregister.
	removalClassLegacyRecovery
	// removalClassFail: ownership is unprovable (dir + .git exist but git
	// cannot list) or a probe errored — surface the error, destroy nothing.
	removalClassFail
)

func (c removalClass) String() string {
	switch c {
	case removalClassResumeMarker:
		return "resume-marker"
	case removalClassForceRemove:
		return "force-remove"
	case removalClassCleanUnregistered:
		return "clean-unregistered"
	case removalClassSucceedAbsent:
		return "succeed-absent"
	case removalClassLegacyRecovery:
		return "legacy-recovery"
	default:
		return "fail"
	}
}

// classifyWorkspaceRemoval is the entry decision: which recovery path the
// sentinel combination selects. Total over every input combination so no
// seventh shape can be invented at the call site; destructive only through
// the single proven-foreign, git-broken combination (see
// WorkspaceRemovalCheck's sweep).
func classifyWorkspaceRemoval(markerPending bool, p removalProbes) removalClass {
	if markerPending {
		return removalClassResumeMarker
	}
	switch p.ownership {
	case ownershipRegistered:
		return removalClassForceRemove
	case ownershipUnregistered:
		return removalClassCleanUnregistered
	}
	switch p.dir {
	case sentinelAbsent:
		return removalClassSucceedAbsent
	case sentinelPresent:
		switch p.gitFile {
		case sentinelAbsent:
			if p.retryMarker {
				return removalClassCleanUnregistered
			}
			if p.legacyManaged {
				return removalClassLegacyRecovery
			}
			return removalClassCleanUnregistered
		default:
			// .git present, or its probe errored: ownership stays unproven —
			// fail closed.
			return removalClassFail
		}
	default:
		return removalClassFail
	}
}

// forceFailureClass re-classifies after `worktree remove --force` failed.
type forceFailureClass uint8

const (
	// forceClassFail: the worktree may still be registered and intact —
	// surface the error, destroy nothing.
	forceClassFail forceFailureClass = iota
	// forceClassContextRecovery (destructive): git itself is broken — the
	// remove never reached the worktree; resume cleanup with unregister.
	forceClassContextRecovery
	// forceClassRemnantRecovery (destructive): git unregistered the worktree
	// (the .git file is gone) but left the directory behind (untracked
	// files) — resume cleanup without unregister.
	forceClassRemnantRecovery
	// forceClassRefuseUnsafe: the .git file is gone but the path failed the
	// safe-cleanup check — refuse with a named error rather than the git
	// error, so the operator sees the real reason.
	forceClassRefuseUnsafe
)

func (c forceFailureClass) String() string {
	switch c {
	case forceClassContextRecovery:
		return "context-recovery"
	case forceClassRemnantRecovery:
		return "remnant-recovery"
	case forceClassRefuseUnsafe:
		return "refuse-unsafe"
	default:
		return "fail"
	}
}

// destructive reports whether the class runs a persistAndResume recovery.
func (c forceFailureClass) destructive() bool {
	return c == forceClassContextRecovery || c == forceClassRemnantRecovery
}

// classifyForceFailure is the second-stage decision. gitContext is
// isGitContextError(err); gitFile is the .git probe after the failure;
// safePath is isSafeWorkspaceCleanupPath(workspacePath). A present or
// errored .git probe fails closed: the worktree may still be registered.
func classifyForceFailure(gitContext bool, gitFile pathSentinel, safePath bool) forceFailureClass {
	if gitContext {
		return forceClassContextRecovery
	}
	if gitFile == sentinelAbsent {
		if safePath {
			return forceClassRemnantRecovery
		}
		return forceClassRefuseUnsafe
	}
	return forceClassFail
}
