package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/andyrewlee/amux/internal/fsm"
)

func workspaceCleanupRetryFingerprintWithContext(ctx context.Context, workspacePath string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(workspacePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()

	hasher := sha256.New()
	retryMetadataPath := workspaceCleanupRetryMetadataPath(workspacePath)
	retryMetadataRel, err := filepath.Rel(workspacePath, retryMetadataPath)
	if err != nil {
		return "", err
	}
	retryMetadataRel = filepath.ToSlash(retryMetadataRel)
	err = fs.WalkDir(root.FS(), ".", func(relPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if relPath == retryMetadataRel {
			return nil
		}
		if relPath == "." && d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(
			hasher,
			"%s|%s|%d|%d\n",
			relPath,
			info.Mode().String(),
			info.Size(),
			info.ModTime().UnixNano(),
		)
		if d.Type()&fs.ModeSymlink != 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			target, err := root.Readlink(filepath.FromSlash(relPath))
			if err != nil {
				return err
			}
			fmt.Fprintf(hasher, "symlink=%s\n", target)
		}
		if relPath == ".git" && !d.IsDir() {
			if err := ctx.Err(); err != nil {
				return err
			}
			content, err := root.ReadFile(filepath.FromSlash(relPath))
			if err != nil {
				return err
			}
			hasher.Write(content)
			hasher.Write([]byte{0})
		}
		if d.IsDir() {
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// The retry-metadata match decision.
//
// A retry-metadata marker gates a DESTRUCTIVE resume (the pending cleanup
// removes the workspace path), so "does this marker still own this path?"
// must not be a handful of inline flag checks. The rule, now explicit:
// a fingerprinted marker owns the path only while the contents still hash
// to the recorded fingerprint; a LEGACY marker (no fingerprint, from before
// the scheme existed) owns it only while the path has no `.git` file — so a
// worktree recreated at the same path is never pruned by the old marker.
// retryGuardCheck verifies the decision tree; the outcome enum's matches()
// is the destructive gate.
type retryMatchOutcome uint8

const (
	// retryNoMatchDirGone: the path is proven absent — nothing to clean.
	retryNoMatchDirGone retryMatchOutcome = iota
	// retryMatchLegacyPending: legacy marker, path exists without .git —
	// the leftover is still the marker's to clean.
	retryMatchLegacyPending
	// retryNoMatchLegacyRecreated: legacy marker but .git exists — a new
	// worktree took the path; the marker no longer owns it.
	retryNoMatchLegacyRecreated
	// retryMatchFingerprint: fingerprinted marker, contents unchanged.
	retryMatchFingerprint
	// retryNoMatchFingerprintDrift: fingerprinted marker, contents changed.
	retryNoMatchFingerprintDrift
)

func (o retryMatchOutcome) String() string {
	switch o {
	case retryMatchLegacyPending:
		return "legacy-pending"
	case retryNoMatchLegacyRecreated:
		return "legacy-recreated"
	case retryMatchFingerprint:
		return "fingerprint-match"
	case retryNoMatchFingerprintDrift:
		return "fingerprint-drift"
	default:
		return "dir-gone"
	}
}

// matches reports whether the marker still owns the path — the only outcome
// shape that may authorize destructive cleanup.
func (o retryMatchOutcome) matches() bool {
	return o == retryMatchLegacyPending || o == retryMatchFingerprint
}

// classifyRetryMetadataMatch reduces the probes to the outcome. dir and
// gitFile are the path/.git sentinels; legacy is metadata.WorkspaceFingerprint == "";
// fingerprintSame is only consulted for fingerprinted markers.
func classifyRetryMetadataMatch(dir, gitFile pathSentinel, legacy, fingerprintSame bool) retryMatchOutcome {
	switch dir {
	case sentinelAbsent:
		return retryNoMatchDirGone
	case sentinelPresent:
		if legacy {
			if gitFile == sentinelAbsent {
				return retryMatchLegacyPending
			}
			return retryNoMatchLegacyRecreated
		}
		if fingerprintSame {
			return retryMatchFingerprint
		}
		return retryNoMatchFingerprintDrift
	default:
		// Unproven presence is not ownership — a marker never matches a path
		// it cannot see (fail closed toward keeping the marker pending).
		return retryNoMatchFingerprintDrift
	}
}

// retryGuardMachine mirrors the decision tree as a machine so its shape is
// Check-verified: both match terminals are leaves, the legacy-match leaf is
// reachable only through a proven-absent .git, and the fingerprint-match
// leaf only through an equal hash.
type retryGuardEvent uint8

const (
	evtRetryDirGone retryGuardEvent = iota
	evtRetryDirPresent
	evtRetryLegacyMarker
	evtRetryFingerprintedMarker
	evtRetryGitFileAbsent
	evtRetryGitFilePresent
	evtRetryFingerprintSame
	evtRetryFingerprintDifferent
)

func (e retryGuardEvent) String() string {
	switch e {
	case evtRetryDirGone:
		return "dir-gone"
	case evtRetryDirPresent:
		return "dir-present"
	case evtRetryLegacyMarker:
		return "legacy-marker"
	case evtRetryFingerprintedMarker:
		return "fingerprinted-marker"
	case evtRetryGitFileAbsent:
		return "git-file-absent"
	case evtRetryGitFilePresent:
		return "git-file-present"
	case evtRetryFingerprintSame:
		return "fingerprint-same"
	default:
		return "fingerprint-different"
	}
}

type retryGuardState uint8

const (
	retryGuardProbe retryGuardState = iota
	retryGuardLegacy
	retryGuardFingerprinted
	retryGuardOutDirGone
	retryGuardOutLegacyPending
	retryGuardOutLegacyRecreated
	retryGuardOutMatch
	retryGuardOutDrift
)

func (s retryGuardState) String() string {
	switch s {
	case retryGuardProbe:
		return "probe"
	case retryGuardLegacy:
		return "legacy"
	case retryGuardFingerprinted:
		return "fingerprinted"
	case retryGuardOutDirGone:
		return "dir-gone"
	case retryGuardOutLegacyPending:
		return "legacy-pending"
	case retryGuardOutLegacyRecreated:
		return "legacy-recreated"
	case retryGuardOutMatch:
		return "fingerprint-match"
	default:
		return "fingerprint-drift"
	}
}

var retryGuardMachine = fsm.MustCompile(fsm.Machine[retryGuardState, retryGuardEvent]{
	Name:    "retry-metadata-guard",
	Initial: retryGuardProbe,
	Steps: []fsm.Step[retryGuardState, retryGuardEvent]{
		{From: retryGuardProbe, Event: evtRetryDirGone, To: retryGuardOutDirGone},
		{From: retryGuardProbe, Event: evtRetryDirPresent, To: retryGuardProbe},
		{From: retryGuardProbe, Event: evtRetryLegacyMarker, To: retryGuardLegacy},
		{From: retryGuardProbe, Event: evtRetryFingerprintedMarker, To: retryGuardFingerprinted},
		// Legacy ownership stops at the first .git file: a recreated
		// worktree is not the marker's leftover.
		{From: retryGuardLegacy, Event: evtRetryGitFileAbsent, To: retryGuardOutLegacyPending},
		{From: retryGuardLegacy, Event: evtRetryGitFilePresent, To: retryGuardOutLegacyRecreated},
		// Fingerprinted ownership follows the contents, not the path.
		{From: retryGuardFingerprinted, Event: evtRetryFingerprintSame, To: retryGuardOutMatch},
		{From: retryGuardFingerprinted, Event: evtRetryFingerprintDifferent, To: retryGuardOutDrift},
	},
	Terminals: []retryGuardState{
		retryGuardOutDirGone, retryGuardOutLegacyPending,
		retryGuardOutLegacyRecreated, retryGuardOutMatch, retryGuardOutDrift,
	},
})

// retryGuardCheck formally verifies the guard: every outcome reachable, the
// two owning (matching) outcomes entered only through their proof (absent
// .git for legacy, equal fingerprint for fingerprinted), and the classifier
// sweep — matches() is true exactly for those two outcomes across the whole
// sentinel space. Runs in git's tests.
func retryGuardCheck() error {
	if err := retryGuardMachine.Check(fsm.Checker[retryGuardState, retryGuardEvent]{
		Alphabet: []retryGuardEvent{
			evtRetryDirGone, evtRetryDirPresent, evtRetryLegacyMarker,
			evtRetryFingerprintedMarker, evtRetryGitFileAbsent, evtRetryGitFilePresent,
			evtRetryFingerprintSame, evtRetryFingerprintDifferent,
		},
		AllDeclaredReachable: true,
		Edge: func(from retryGuardState, event retryGuardEvent, to retryGuardState) error {
			if to == retryGuardOutLegacyPending && event != evtRetryGitFileAbsent {
				return fmt.Errorf("legacy ownership via %v: only a proven-absent .git leaves a legacy marker owning the path", event)
			}
			if to == retryGuardOutMatch && event != evtRetryFingerprintSame {
				return fmt.Errorf("fingerprint ownership via %v: only an equal fingerprint owns the path", event)
			}
			return nil
		},
	}); err != nil {
		return err
	}
	for _, dir := range []pathSentinel{sentinelPresent, sentinelAbsent, sentinelError, sentinelUnknown} {
		for _, gitFile := range []pathSentinel{sentinelPresent, sentinelAbsent, sentinelError, sentinelUnknown} {
			for _, legacy := range []bool{false, true} {
				for _, same := range []bool{false, true} {
					got := classifyRetryMetadataMatch(dir, gitFile, legacy, same)
					// Expected outcome, derived from the rule directly:
					// ownership needs a present dir plus its proof.
					want := retryNoMatchFingerprintDrift
					switch dir {
					case sentinelAbsent:
						want = retryNoMatchDirGone
					case sentinelPresent:
						switch {
						case legacy && gitFile == sentinelAbsent:
							want = retryMatchLegacyPending
						case legacy:
							want = retryNoMatchLegacyRecreated
						case same:
							want = retryMatchFingerprint
						}
					}
					if got != want {
						return fmt.Errorf("classify(dir=%v gitFile=%v legacy=%v same=%v) = %v, want %v",
							dir, gitFile, legacy, same, got, want)
					}
					if got.matches() != (want == retryMatchLegacyPending || want == retryMatchFingerprint) {
						return fmt.Errorf("classify(dir=%v gitFile=%v legacy=%v same=%v): matches() must hold exactly for the two owning outcomes",
							dir, gitFile, legacy, same)
					}
				}
			}
		}
	}
	return nil
}

func workspacePathMatchesRetryMetadataCleanup(workspacePath string, metadata workspaceCleanupRetryMetadata) (bool, error) {
	return workspacePathMatchesRetryMetadataCleanupWithContext(context.Background(), workspacePath, metadata)
}

func workspacePathMatchesRetryMetadataCleanupWithContext(
	ctx context.Context,
	workspacePath string,
	metadata workspaceCleanupRetryMetadata,
) (bool, error) {
	dir, err := probeSentinel(workspacePath)
	if err != nil {
		return false, err
	}
	legacy := metadata.WorkspaceFingerprint == ""
	if !legacy {
		// Fingerprinted markers compare against the live contents; an
		// unreadable path is an error, not a drift claim.
		currentFingerprint, fpErr := ops.workspaceCleanupRetryFingerprintCtx(ctx, workspacePath)
		if fpErr != nil {
			return false, fpErr
		}
		return classifyRetryMetadataMatch(dir, sentinelUnknown, legacy,
			currentFingerprint == metadata.WorkspaceFingerprint).matches(), nil
	}
	gitFile, gitErr := probeSentinel(filepath.Join(workspacePath, ".git"))
	if gitErr != nil {
		return false, gitErr
	}
	return classifyRetryMetadataMatch(dir, gitFile, legacy, false).matches(), nil
}

func rejectReusedWorkspacePathForRetryMetadataCleanup(workspacePath string, metadata workspaceCleanupRetryMetadata) error {
	matches, err := workspacePathMatchesRetryMetadataCleanup(workspacePath, metadata)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("workspace path %s exists while pending cleanup remains", workspacePath)
	}
	return nil
}
