package data

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/logging"
)

// RebindRoot updates a record's Root in place under its existing store key —
// the intentional identity migration a project rename performs after moving
// the worktree directory (git worktree move). Unlike Update it is allowed to
// change Root; Repo never changes, and the store key stays put so every
// durable artifact keyed on the ID — tmux session names and tags, port
// reservations, delete markers — survives the move.
//
// The new root must be a non-empty absolute path distinct from the record's
// repo (primary checkouts are their own root and never move) and must not be
// claimed by another record's repo+root identity. Rebinding to the current
// root is a no-op so a partially completed rename can be retried wholesale.
func (s *WorkspaceStore) RebindRoot(id WorkspaceID, newRoot string) error {
	if err := validateWorkspaceID(id); err != nil {
		return err
	}
	newRoot = strings.TrimSpace(newRoot)
	if newRoot == "" {
		return fmt.Errorf("rebind workspace %s: new root is required", id)
	}
	if !filepath.IsAbs(newRoot) {
		return fmt.Errorf("rebind workspace %s: new root %q must be an absolute path", id, newRoot)
	}
	cleaned := filepath.Clean(newRoot)

	lockFiles, err := s.lockWorkspaceIDs(id)
	if err != nil {
		return err
	}
	defer unlockRegistryFiles(lockFiles)

	ws, err := s.load(id, true)
	if err != nil {
		return fmt.Errorf("rebind workspace %s: %w", id, err)
	}
	ws.storeID = id

	if ws.Root == cleaned {
		return nil
	}
	if strings.TrimSpace(ws.Repo) == cleaned {
		return fmt.Errorf("rebind workspace %s: new root %s is the repo's primary checkout", id, cleaned)
	}

	// Refuse to shadow another live record's identity: two records claiming
	// the same repo+root would make findStoredWorkspace ambiguous and let a
	// later save delete the loser's metadata. The comparison also folds
	// case: on the case-insensitive filesystems the workspaces root commonly
	// lives on, two spellings of one physical directory compare unequal
	// under the exact-match lookup, so a case-variant record would slip the
	// check and split the worktree's metadata.
	if other, otherID, err := s.findStoredWorkspace(ws.Repo, cleaned); err != nil {
		return fmt.Errorf("rebind workspace %s: %w", id, err)
	} else if other != nil && otherID != id {
		return fmt.Errorf("rebind workspace %s: root %s is already recorded by workspace %s", id, cleaned, otherID)
	}
	if err := s.refuseCaseFoldedRootCollision(ws.Repo, cleaned, id); err != nil {
		return fmt.Errorf("rebind workspace %s: %w", id, err)
	}

	ws.Root = cleaned
	if err := validateWorkspaceForSave(ws); err != nil {
		return fmt.Errorf("rebind workspace %s: %w", id, err)
	}
	if err := s.saveWorkspaceLocked(id, ws); err != nil {
		return fmt.Errorf("rebind workspace %s: %w", id, err)
	}
	return nil
}

// refuseCaseFoldedRootCollision rejects a rebind whose target root matches
// another record's root ignoring letter case. On the case-insensitive
// filesystems the workspaces root commonly lives on, two spellings of one
// physical directory compare unequal under the exact-match lookup above, so
// a case-variant record would slip it and both records would end up
// claiming one worktree. The refusal is deliberately conservative on
// case-sensitive filesystems too — the collision is unlikely there, but a
// false negative is data corruption and a false positive is a clean retry.
func (s *WorkspaceStore) refuseCaseFoldedRootCollision(repo, newRoot string, selfID WorkspaceID) error {
	ids, err := s.List()
	if err != nil {
		return fmt.Errorf("scan records for case-variant root collision: %w", err)
	}
	targetRepo := canonicalLookupPath(repo)
	targetRoot := canonicalLookupPath(newRoot)
	for _, id := range ids {
		if id == selfID {
			continue
		}
		// applyDefaults=false: only identity fields are compared; an
		// unreadable record (mid-write elsewhere) skips rather than fails —
		// the exact-match check above remains the hard gate.
		ws, err := s.load(id, false)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				logging.Warn("Skipping unreadable workspace metadata %s during case-variant root check: %v", id, err)
			}
			continue
		}
		if canonicalLookupPath(ws.Repo) != targetRepo {
			continue
		}
		if strings.EqualFold(canonicalLookupPath(ws.Root), targetRoot) {
			// Case-variant spelling: refuse only when both directories
			// exist and are physically the same directory — the
			// case-insensitive filesystem case this check exists for. On a
			// case-sensitive filesystem, distinct case-variant worktrees
			// ("feature" / "FEATURE" as distinct directories) are
			// legitimate, and refusing would wedge the rename permanently:
			// the colliding record stays parked at its own case-variant
			// destination and every retry fails identically.
			if same, ok := SamePhysicalDir(ws.Root, newRoot); ok && same {
				return fmt.Errorf("root %s is already recorded by workspace %s (case-insensitive match)", newRoot, id)
			}
		}
	}
	return nil
}

// SamePhysicalDir reports whether two paths denote the same directory on
// disk (os.SameFile: inode identity, so case-variant spellings of one
// directory on a case-insensitive filesystem compare equal, while distinct
// case-variant directories on a case-sensitive filesystem do not). ok is
// false when either path does not exist or cannot be stat-ed.
func SamePhysicalDir(a, b string) (same, ok bool) {
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false, false
	}
	if !infoA.IsDir() || !infoB.IsDir() {
		return false, true
	}
	return os.SameFile(infoA, infoB), true
}
