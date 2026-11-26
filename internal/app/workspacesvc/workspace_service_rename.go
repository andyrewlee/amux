package workspacesvc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
)

// RenameProject renames a project and migrates its managed worktrees to the
// new container. The ordering is chosen so a partial failure is always safe
// to retry:
//
//  1. The registry rename lands first. The new name immediately becomes the
//     authoritative workspaces-root segment; worktrees still sitting in the
//     old container keep working through the path-basename aliases every
//     managed-root check accepts, so nothing is orphaned mid-migration.
//  2. Each managed worktree is moved with git worktree move and its record
//     rebound in place under the same store key — tmux sessions, tags, and
//     port reservations survive untouched.
//  3. Shelved records get a metadata-only rebind: the worktree is already
//     gone, and the restored worktree will attach at the new path.
//
// Workspaces with hosted script sessions running are skipped (a warning
// counts them); a failed move or rebind rolls that one worktree back and the
// run continues. The returned message reports moved/skipped/failed counts so
// the UI can tell the user exactly what remains.
//
// Re-issuing the rename with the name the project already holds skips the
// registry write and reconciles only: worktrees without records still move
// with their container (a rescan creates their records at the new root), and
// anything an interrupted run left behind finishes.
//
// The migration holds the service's migration lock, so rescans, prunes, and
// loads issued while it runs wait instead of racing the move-then-rebind
// window (a rescan would see the moved worktree with no record at the new
// root and mint a duplicate; a prune would delete the old-root record as a
// missing root). The lock fences this process only — a rename from the CLI
// racing another amux process is still the user's to sequence.
func (s *Service) RenameProject(project *data.Project, newName string) tea.Cmd {
	return func() tea.Msg {
		if project == nil {
			return messages.Error{Err: errors.New("missing project"), Context: workspaceErrContext("renaming project")}
		}
		if s == nil || s.registry == nil {
			return messages.Error{Err: errors.New("registry unavailable"), Context: workspaceErrContext("renaming project")}
		}
		newName = strings.TrimSpace(newName)
		oldName := strings.TrimSpace(project.Name)
		sameName := newName == oldName
		if !sameName && strings.EqualFold(newName, oldName) {
			// A case-only rename. Where the filesystem is case-insensitive
			// both spellings name the SAME container directory, and the
			// store's and git's identity comparisons are case-sensitive —
			// records and worktree registrations would end up split across
			// two spellings of one path. Refuse that combination; where the
			// filesystem distinguishes the names the rename proceeds below.
			root := strings.TrimSpace(s.workspacesRoot)
			if same, ok := data.SamePhysicalDir(filepath.Join(root, oldName), filepath.Join(root, newName)); ok && same {
				return messages.Error{
					Err: fmt.Errorf("renaming %q to %q only changes letter case and both spellings resolve to the same directory under %s; record and git identities are case-sensitive — choose a different name",
						oldName, newName, root),
					Context: workspaceErrContext("renaming project"),
				}
			}
		}
		renamed := data.NewProjectNamed(newName, project.Path)
		if !sameName {
			// Registry first: the name is validated (safe segment, unique
			// among the other registered projects) and persisted atomically.
			if err := s.registry.RenameProject(project.Path, newName); err != nil {
				return messages.Error{Err: err, Context: workspaceErrContext("renaming project")}
			}
		}

		moved, skipped, failed, err := s.migrateProjectWorktrees(project, renamed)

		if err != nil {
			// User-visible operation partially failed and the app
			// continued — the error level the log boundary reserves for
			// exactly this, with the counts that say what remains.
			logging.Error("Renamed project %s (%s -> %s) but the migration did not finish: moved=%d skipped=%d failed=%d: %v",
				project.Path, oldName, newName, moved, skipped, failed, err)
		} else {
			logging.Info("Renamed project %s (%s -> %s): moved=%d skipped=%d failed=%d",
				project.Path, oldName, newName, moved, skipped, failed)
		}
		return messages.ProjectRenamed{
			Path:    project.Path,
			OldName: oldName,
			NewName: newName,
			Moved:   moved,
			Skipped: skipped,
			Failed:  failed,
			Err:     err,
		}
	}
}

// renameProgress accumulates one migration run's counts and errors.
type renameProgress struct {
	moved, skipped, failed int
	errs                   []error
}

func (p *renameProgress) record(format string, args ...any) {
	p.errs = append(p.errs, fmt.Errorf(format, args...))
}

// migrateProjectWorktrees moves the project's managed worktrees into the
// renamed project's container and rebinds their records, then rebinds the
// shelved records' roots. Returns moved/skipped/failed counts and a joined
// error describing every failure (nil when nothing failed).
func (s *Service) migrateProjectWorktrees(old, renamed *data.Project) (moved, skipped, failed int, err error) {
	if s.store == nil || s.gitOps == nil {
		return 0, 0, 0, nil
	}
	// Fence the move-then-rebind window against this process's rescans,
	// prunes, and loads — see RenameProject's doc comment.
	s.migrationMu.Lock()
	defer s.migrationMu.Unlock()

	// Only this project's worktrees that sit in a project-shaped container
	// move: flat-layout worktrees (<root>/<leaf>) predate per-project
	// containers and are not namespaced by the project name, and foreign
	// repos' worktrees never belong to this project. The inOld predicate
	// below encodes that container shape.
	newContainer := filepath.Join(strings.TrimSpace(s.workspacesRoot), renamed.Name)
	if err := os.MkdirAll(newContainer, 0o700); err != nil {
		return 0, 0, 0, fmt.Errorf("create container %s: %w", newContainer, err)
	}
	// Containers other registered projects own — a worktree of this repo
	// sitting under one of those is not ours to relocate.
	claimed := s.claimedContainerSegments(old)

	discovered, discoverErr := s.gitOps.DiscoverWorkspaces(old)
	if discoverErr != nil {
		// Discovery failure blocks the moves but not the rename itself —
		// the registry already committed and old locations keep working.
		var prog renameProgress
		prog.record("discover workspaces for %s: %w", old.Path, discoverErr)
		return 0, 0, 0, errors.Join(prog.errs...)
	}

	var prog renameProgress
	// The record index drives every record reconciliation below: a record
	// stranded by an interrupted run (git moved the worktree, the rebind
	// never ran) claims a historical spelling of the container that no
	// current name can derive, so records are found by leaf.
	records, recordsErr := s.loadRenameRecords(old, renamed, claimed)
	if recordsErr != nil {
		// Record reconciliation is blocked, but the moves are still safe —
		// old locations keep working through path-basename aliases, and a
		// later retry with the store readable finishes the rebinds.
		prog.record("list records for %s: %w", old.Path, recordsErr)
	}
	s.migrateDiscoveredWorktrees(old, renamed, newContainer, claimed, discovered, records, &prog)
	s.migrateShelvedRecords(old, renamed, newContainer, claimed, &prog)
	return prog.moved, prog.skipped, prog.failed, errors.Join(prog.errs...)
}

// migrateDiscoveredWorktrees runs the per-worktree loop for a rename
// migration: same-place and interrupted shapes reconcile records, container
// children move (skipping those with hosted scripts running), and anything
// else stays put.
func (s *Service) migrateDiscoveredWorktrees(old, renamed *data.Project, newContainer string, claimed map[string]bool, discovered []data.Workspace, records *renameRecords, prog *renameProgress) {
	// Claim pass: every discovered worktree's exact record claim is served
	// before any move runs, so the stranded-record fallback below — whose
	// dead-root rule cannot tell a move's strand from a removal's leftover
	// — is never order-dependent against an owner that still claims its
	// record. Leaf collisions between discovered worktrees are marked too:
	// their destination slot is ambiguous, so no fallback below may assume
	// the directory or record there belongs to any one of them.
	claims, contestedLeaf := s.claimRecords(discovered, old, records)
	for i := range discovered {
		ws := &discovered[i]
		if ws.IsPrimaryCheckout() {
			continue
		}
		// Repo identity comes before any path shape: only worktrees this
		// project's git actually registered move. (Discovery normally
		// guarantees this; the guard keeps a drifted or forged entry from
		// dragging another repo's worktree into our container.)
		if !data.SamePath(ws.Repo, old.Path) {
			continue
		}

		leaf := filepath.Base(ws.Root)
		dest := filepath.Join(newContainer, leaf)
		// The worktree already lives where the new name puts it — the
		// second half of a basename collision (the first repo moved to
		// checkfu-old; this one renames back to the bare basename whose
		// container its worktrees never left). No move happens; only a
		// record still keyed to a historical spelling may need work.
		if data.SamePath(ws.Root, dest) {
			reconciled, err := s.finishInterruptedMove(ws, claims[ws], records, contestedLeaf[leaf])
			if err != nil {
				prog.failed++
				prog.record("%s", err)
				continue
			}
			if reconciled {
				prog.moved++
			}
			continue
		}
		// The old project's managed roots (stored name + path-basename
		// aliases) name the containers to migrate FROM, plus any
		// project-shaped segment under the workspaces root that no OTHER
		// registered project claims — the retry story: a first run
		// interrupted by a skip, a failure, or a crash leaves worktrees in
		// the retired old-name container, and the retry's caller already
		// carries the NEW name, so the retired segment is nobody's alias.
		// The new container is compared directly for the interrupted case.
		inOld := isManagedWorkspaceChildPathForProject(s.workspacesRoot, old, ws.Root) ||
			s.isUnclaimedContainerChild(claimed, renamed.Name, ws.Root)
		inNew := pathWithinAliasesImmediateChild(workspacePathAliases(newContainer), workspacePathAliases(ws.Root))

		if inNew && !inOld {
			// A prior run moved this worktree (git's registration already
			// points here); only a record still keyed to a historical
			// spelling can lag. Finish it and move on.
			reconciled, err := s.finishInterruptedMove(ws, claims[ws], records, contestedLeaf[leaf])
			if err != nil {
				prog.failed++
				prog.record("%s", err)
				continue
			}
			if reconciled {
				prog.moved++
			}
			continue
		}
		if !inOld {
			// Flat-layout worktrees (<root>/<leaf>) predate per-project
			// containers and are not namespaced by the project name.
			continue
		}

		if s.scriptsRunning != nil && s.scriptsRunning(ws) {
			// A hosted script session holds the worktree open; move it under
			// a running agent only with the user's say-so. Skipped, not
			// failed: the rename still applies and a retry moves the rest.
			logging.Warn("Skipping %s during rename: scripts are running in it", ws.Root)
			prog.skipped++
			continue
		}

		if did, migrateErr := s.migrateOneWorktree(old, ws, dest, claims[ws], records, contestedLeaf[leaf]); migrateErr != nil {
			prog.failed++
			prog.record("%s", migrateErr)
		} else if did {
			prog.moved++
		}
	}
}

// claimRecords serves every discovered worktree's exact record claim: the
// record already claiming a worktree's root is removed from the index and
// returned keyed by that worktree. Runs before any move so a later
// dead-root fallback can never take a record its owner still claims. It
// also marks every workspace leaf shared by more than one discovered
// worktree: their destination slots are contested, and nothing downstream
// may assume the directory that lands there belongs to any one of them.
func (s *Service) claimRecords(discovered []data.Workspace, old *data.Project, records *renameRecords) (map[*data.Workspace]*data.Workspace, map[string]bool) {
	claims := make(map[*data.Workspace]*data.Workspace)
	leafCounts := make(map[string]int)
	for i := range discovered {
		ws := &discovered[i]
		if ws.IsPrimaryCheckout() || !data.SamePath(ws.Repo, old.Path) {
			continue
		}
		leafCounts[filepath.Base(filepath.Clean(strings.TrimSpace(ws.Root)))]++
		if rec := records.claimExact(ws); rec != nil {
			claims[ws] = rec
		}
	}
	contestedLeaf := make(map[string]bool)
	for leaf, n := range leafCounts {
		if n > 1 {
			contestedLeaf[leaf] = true
		}
	}
	return claims, contestedLeaf
}

// migrateShelvedRecords rebinds the shelved records whose roots live in the
// project's migration sources: metadata-only, the restore path attaches at
// the new container. A listing failure fails the migration — the shelved
// records' roots would silently stay in the retired containers.
func (s *Service) migrateShelvedRecords(old, renamed *data.Project, newContainer string, claimed map[string]bool, prog *renameProgress) {
	shelved, listErr := s.listShelvedWorkspaces(nil, old.Path)
	if listErr != nil {
		prog.record("list shelved workspaces for %s: %w", old.Path, listErr)
		return
	}
	for i := range shelved {
		ws := &shelved[i]
		if !isManagedWorkspaceChildPathForProject(s.workspacesRoot, old, ws.Root) &&
			!s.isUnclaimedContainerChild(claimed, renamed.Name, ws.Root) {
			continue
		}
		dest := filepath.Join(newContainer, filepath.Base(ws.Root))
		if data.SamePath(ws.Root, dest) {
			// Already at the destination (a same-name retry) — no work, and
			// the moved count counts actual work.
			continue
		}
		if rebindErr := s.rebindWorkspaceRoot(ws, dest); rebindErr != nil {
			prog.failed++
			prog.record("%s", rebindErr)
		} else {
			prog.moved++
		}
	}
}

// claimedContainerSegments maps (lowercased) workspaces-root segments owned
// by OTHER registered projects: a worktree of this repo sitting under one of
// them is not ours to relocate. A registry read failure degrades to "nothing
// claimed" — the alias check still applies, so the extra migration sources
// just widen, never narrow, in that case.
func (s *Service) claimedContainerSegments(self *data.Project) map[string]bool {
	claimed := make(map[string]bool)
	if s.registry == nil {
		return claimed
	}
	entries, err := s.registry.Entries()
	if err != nil {
		logging.Warn("Rename migration could not read the registry to classify containers; migrating the documented aliases only: %v", err)
		return claimed
	}
	for _, entry := range entries {
		if data.SamePath(entry.Path, self.Path) {
			continue
		}
		claimed[strings.ToLower(strings.TrimSpace(entry.Name))] = true
	}
	return claimed
}

// isUnclaimedContainerChild reports whether root sits one level below an
// unclaimed project-shaped container: <workspaces-root>/<segment>/<leaf>
// where segment is neither the renamed project's new name nor a segment
// another registered project owns. Direct children of the workspaces root
// itself (the flat layout) are never container children — and neither is
// anything outside the workspaces root or nested deeper: discovery lists
// every worktree git has registered for the repo wherever it lives, so
// without the immediate-child bound the sweep would relocate a repo's
// out-of-root worktrees (~/Code/wip) into the renamed container.
func (s *Service) isUnclaimedContainerChild(claimed map[string]bool, renamedName, root string) bool {
	cleaned := filepath.Clean(strings.TrimSpace(root))
	parent := filepath.Dir(cleaned)
	if !pathWithinAliasesImmediateChild(workspacePathAliases(s.workspacesRoot), workspacePathAliases(parent)) {
		return false
	}
	segment := strings.ToLower(filepath.Base(parent))
	return segment != strings.ToLower(strings.TrimSpace(renamedName)) && !claimed[segment]
}

// migrateOneWorktree brings one worktree into the renamed container and
// leaves its record consistent with the new location. It reports whether
// this call did work (a move or a record rebind — the moved count counts
// actual work, not no-ops). Three shapes:
//
//   - Source present, destination free: the normal path — move with git
//     worktree move, then rebind the record in place under its store key.
//     A rebind failure rolls the move back so the record and the
//     filesystem never disagree.
//
//   - Source gone, destination present: a previous run's interrupted move.
//     Rebind the record when one still points at the worktree's stale
//     registration or a dead historical spelling; otherwise the worktree
//     is already fully consistent (or never had a record — the next rescan
//     creates it at the new root). Idempotent. Unless the leaf is
//     contested: something occupies the destination, but with two
//     same-named worktrees it cannot be assumed to be THIS worktree's
//     directory — rebinding would attach this workspace's identity to the
//     sibling's content, so the collision is reported instead.
//
//   - Neither path exists: the worktree was removed out-of-band and git's
//     registration is stale. Leave the record and the registration exactly
//     as they are — rebinding the record to a path nothing will ever create
//     would strand it; git worktree prune and the next rescan reconcile
//     the pair on their own.
//
// Records are matched only after the source/destination checks pass: a
// worktree that ends up untouched (out-of-band removal) or refused (the
// destination is occupied) must not consume a record another worktree is
// owed.
func (s *Service) migrateOneWorktree(project *data.Project, ws *data.Workspace, dest string, claimed *data.Workspace, records *renameRecords, contestedLeaf bool) (bool, error) {
	_, destExistsErr := os.Stat(dest)
	destExists := destExistsErr == nil
	sourceExists := false
	if _, err := os.Stat(ws.Root); err == nil {
		sourceExists = true
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("rename %s: stat source: %w", ws.Root, err)
	}

	if !sourceExists {
		if !destExists {
			// Out-of-band removal: git's registration is stale and the
			// record (if any) still matches it — leave both alone.
			return false, nil
		}
		if contestedLeaf {
			// Another discovered worktree shares this leaf: the directory
			// at the destination may be the sibling's, not this
			// worktree's — binding this worktree's record to it would
			// attach the wrong identity. Report the collision and let the
			// user rename one of them.
			return false, fmt.Errorf("rename %s: destination %s is occupied by another worktree named %q; rename one of them", ws.Root, dest, ws.Name)
		}
		// Interrupted prior move: the directory is already in the new
		// container; only the record may lag. The worktree occupies the
		// destination, so a stranded record may follow it.
		rec := claimed
		if rec == nil {
			rec = records.matchDeadRoot(ws)
		}
		return s.rebindRecordTo(rec, dest)
	}
	if destExists {
		return false, fmt.Errorf("rename %s: destination %s already exists and source is still present", ws.Root, dest)
	}

	// The record that follows this worktree: its exact claim, else — only
	// when the leaf is uncontested and the worktree sits in a
	// project-owned container — a stranded record (a historical spelling
	// an interrupted run left the record at). nil means recordless: the
	// worktree still moves with its container, and the next rescan creates
	// a record at the new root. A recordless worktree in some other
	// unclaimed container gets no fallback, and a contested leaf gets none
	// either: with two same-named worktrees the code cannot tell whose
	// record a dead spelling is, so it is left for the user to
	// disambiguate and a later retry to reconcile.
	rec := claimed
	if rec == nil && contestedLeaf {
		// Deferred, not silent: say so when a stranded record is waiting
		// on the disambiguation.
		s.warnDeferredStrandedRecord(ws, records)
	}
	if rec == nil && !contestedLeaf && isManagedWorkspaceChildPathForProject(s.workspacesRoot, project, ws.Root) {
		rec = records.matchDeadRoot(ws)
	}

	unlockMove := s.lockRepoGit(project.Path)
	moveErr := s.gitOps.MoveWorktree(project.Path, ws.Root, dest)
	unlockMove()
	if moveErr != nil {
		return false, fmt.Errorf("rename %s: move worktree: %w", ws.Root, moveErr)
	}

	if rec == nil {
		// Nothing to rebind — the next rescan creates the record at the
		// new root. Failure to rebind an absent record is not a failure of
		// the move.
		return true, nil
	}

	if rebindErr := s.rebindWorkspaceRoot(rec, dest); rebindErr != nil {
		// Roll the move back so the record and the filesystem agree; the
		// workspace stays in the old (alias-accepted) container.
		unlockRollback := s.lockRepoGit(project.Path)
		rollbackErr := s.gitOps.MoveWorktree(project.Path, dest, ws.Root)
		unlockRollback()
		if rollbackErr != nil {
			return false, errors.Join(rebindErr, fmt.Errorf("rollback move after rebind failure: %w", rollbackErr))
		}
		return false, rebindErr
	}
	return true, nil
}
