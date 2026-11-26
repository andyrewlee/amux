package workspacesvc

// The record half of a project-rename migration — the record index the
// migration reconciles through, and the rebind helpers it dispatches to.
// Split from workspace_service_rename.go under the 500-line cap.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
)

// renameRecords indexes the project's migration-source records by workspace
// leaf — the workspace's stable directory name. The record half of an
// interrupted migration (git moved the worktree, the rebind never ran)
// claims a historical spelling of the container that neither the old nor
// the new project name can derive, so lookups go by leaf, not by path.
// Shelved records are excluded: their reconciliation is
// migrateShelvedRecords' to own.
type renameRecords struct {
	byLeaf map[string][]*data.Workspace
}

// claimExact removes and returns the record already claiming the
// workspace's root — the worktree's exact match, served in the migration's
// claim pass before any move runs, so the order-dependent stranded-record
// fallback below can never take a record its owner still claims (an
// out-of-band-removed worktree's record, for instance).
func (r *renameRecords) claimExact(ws *data.Workspace) *data.Workspace {
	if r == nil {
		return nil
	}
	leaf := filepath.Base(filepath.Clean(strings.TrimSpace(ws.Root)))
	for i, rec := range r.byLeaf[leaf] {
		if data.SamePath(rec.Root, ws.Root) {
			r.remove(leaf, i)
			return rec
		}
	}
	return nil
}

// matchDeadRoot removes and returns the first leaf match whose own root is
// gone from disk — the stranded shape, where the worktree moved away and
// left the record pointing at a directory that no longer exists. Callers
// gate this: only a worktree occupying its destination or sitting in a
// project-owned container may claim a stranded record, so a recordless
// worktree in some other unclaimed container never inherits a neighbor's
// identity, whichever order discovery reports.
func (r *renameRecords) matchDeadRoot(ws *data.Workspace) *data.Workspace {
	if r == nil {
		return nil
	}
	leaf := filepath.Base(filepath.Clean(strings.TrimSpace(ws.Root)))
	for i, rec := range r.byLeaf[leaf] {
		if rootDirExists(rec.Root) {
			// A live sibling's record stays with the worktree still sitting
			// on it.
			continue
		}
		r.remove(leaf, i)
		return rec
	}
	return nil
}

// remove drops the i-th record of a leaf from the index — the record has
// followed a worktree and can serve no other.
func (r *renameRecords) remove(leaf string, i int) {
	candidates := r.byLeaf[leaf]
	rest := make([]*data.Workspace, 0, len(candidates)-1)
	rest = append(rest, candidates[:i]...)
	rest = append(rest, candidates[i+1:]...)
	if len(rest) == 0 {
		delete(r.byLeaf, leaf)
	} else {
		r.byLeaf[leaf] = rest
	}
}

// rootDirExists reports whether the path still exists on disk.
func rootDirExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// loadRenameRecords builds the migration's record index: this repo's
// unshelved records whose roots sit in the project's migration sources (the
// old container and its aliases, plus unclaimed project-shaped containers —
// the retired spellings an interrupted run leaves behind).
func (s *Service) loadRenameRecords(old, renamed *data.Project, claimed map[string]bool) (*renameRecords, error) {
	if s.store == nil {
		return nil, nil
	}
	all, err := s.listByRepoFromSet(nil, old.Path, true)
	if err != nil {
		return nil, err
	}
	idx := &renameRecords{byLeaf: make(map[string][]*data.Workspace, len(all))}
	for _, rec := range all {
		if rec == nil || rec.Root == "" || rec.IsPrimaryCheckout() {
			// The primary checkout never relocates with the container, so
			// its record (when one exists) must not follow a worktree.
			continue
		}
		if rec.RecordLifecycleState(data.WorktreeUnknown).Shelved() {
			continue
		}
		if !isManagedWorkspaceChildPathForProject(s.workspacesRoot, old, rec.Root) &&
			!s.isUnclaimedContainerChild(claimed, renamed.Name, rec.Root) {
			continue
		}
		leaf := filepath.Base(filepath.Clean(strings.TrimSpace(rec.Root)))
		idx.byLeaf[leaf] = append(idx.byLeaf[leaf], rec)
	}
	return idx, nil
}

// hasStrandedCandidate reports whether an unclaimed dead-root record keyed
// to this worktree's leaf still sits in the index — a reconciliation that a
// contested leaf deferred. Peeks without consuming.
func (r *renameRecords) hasStrandedCandidate(ws *data.Workspace) bool {
	if r == nil {
		return false
	}
	leaf := filepath.Base(filepath.Clean(strings.TrimSpace(ws.Root)))
	for _, rec := range r.byLeaf[leaf] {
		if rec != nil && !rootDirExists(rec.Root) {
			return true
		}
	}
	return false
}

// warnDeferredStrandedRecord surfaces a reconciliation deferred by a
// contested leaf: the record stays at its dead spelling until the user
// renames one of the same-named worktrees and re-runs, so without a warn
// the deferral is indistinguishable from a clean run. Degraded but
// recoverable — the Warn tier.
func (s *Service) warnDeferredStrandedRecord(ws *data.Workspace, records *renameRecords) {
	if records.hasStrandedCandidate(ws) {
		logging.Warn("rename: deferring stranded record for %s root=%s reason=another worktree named %q shares the destination; rename one of them and re-run", ws.Name, ws.Root, ws.Name)
	}
}

// rebindRecordTo rebinds the record to dest unless it already claims it.
// Reports whether a rebind actually happened.
func (s *Service) rebindRecordTo(rec *data.Workspace, dest string) (bool, error) {
	if rec == nil || data.SamePath(rec.Root, dest) {
		return false, nil
	}
	if err := s.rebindWorkspaceRoot(rec, dest); err != nil {
		return false, err
	}
	return true, nil
}

// finishInterruptedMove reconciles the record for a worktree that discovery
// already reports in place (either at the new container after a prior run's
// move, or already living where the new name puts it): a record already
// claiming the live root (the claim pass's exact match) is consistent, a
// record still keyed to a dead historical spelling — matched by leaf so a
// retry carrying the current registry name still finds it — rebinds to the
// live root, and no record at all means the next rescan creates one in
// place. A contested leaf (another discovered worktree shares the name)
// skips the fallback: with two same-named worktrees the code cannot tell
// whose record a dead spelling is, so it is left for a later retry, after
// the user disambiguates. Returns whether a stale record was rebound.
func (s *Service) finishInterruptedMove(ws, claimed *data.Workspace, records *renameRecords, contestedLeaf bool) (bool, error) {
	if claimed != nil {
		// The record already claims the live root — consistent.
		return false, nil
	}
	if contestedLeaf {
		// Deferred, not refused: say so when something is actually waiting
		// on the disambiguation.
		s.warnDeferredStrandedRecord(ws, records)
		return false, nil
	}
	rec := records.matchDeadRoot(ws)
	if rec == nil {
		return false, nil
	}
	if err := s.rebindWorkspaceRoot(rec, ws.Root); err != nil {
		return false, err
	}
	return true, nil
}

// rebindWorkspaceRoot updates the record's Root in place under its store key.
func (s *Service) rebindWorkspaceRoot(ws *data.Workspace, newRoot string) error {
	if s.store == nil {
		return nil
	}
	id := ws.MetadataID()
	if err := s.store.RebindRoot(id, newRoot); err != nil {
		return fmt.Errorf("rebind workspace %s to %s: %w", id, newRoot, err)
	}
	ws.Root = newRoot
	return nil
}
