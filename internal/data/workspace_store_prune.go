package data

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WorkspacePruneOptions describes state that is safe for the metadata store to
// reconcile. Pruning only removes amux-owned metadata and lock files; it never
// removes a workspace root or repository.
type WorkspacePruneOptions struct {
	RegisteredRepos   []string
	ManagedRoot       string
	Now               time.Time
	OrphanGracePeriod time.Duration
	ArchivedRetention time.Duration
}

// WorkspacePruneResult reports what a reconciliation pass removed.
type WorkspacePruneResult struct {
	UnregisteredRemoved int
	MissingRootRemoved  int
	ArchivedRemoved     int
	OrphanLocksRemoved  int
}

// MetadataRemoved returns the total number of workspace metadata records
// removed by a reconciliation pass.
func (r WorkspacePruneResult) MetadataRemoved() int {
	return r.UnregisteredRemoved + r.MissingRootRemoved + r.ArchivedRemoved
}

// DeleteByRepo removes every metadata record for repoPath and returns the
// workspace IDs whose sessions should be cleaned. Workspace roots are
// deliberately untouched.
func (s *WorkspaceStore) DeleteByRepo(repoPath string) ([]WorkspaceID, error) {
	if s == nil {
		return nil, nil
	}
	targetRepo := canonicalLookupPath(repoPath)
	if targetRepo == "" {
		return nil, errors.New("repo path is required")
	}
	ids, err := s.List()
	if err != nil {
		return nil, err
	}

	removed := make([]WorkspaceID, 0)
	var errs []error
	for _, id := range ids {
		ws, loadErr := s.Load(id)
		if loadErr != nil {
			errs = append(errs, fmt.Errorf("load workspace %s: %w", id, loadErr))
			continue
		}
		if canonicalLookupPath(ws.Repo) != targetRepo {
			continue
		}
		if deleteErr := s.Delete(id); deleteErr != nil {
			errs = append(errs, fmt.Errorf("delete workspace metadata %s: %w", id, deleteErr))
			continue
		}
		removed = append(removed, id)
		// A loaded record can live under a legacy metadata directory while its
		// canonical repo/root identity produces a newer ID. Sessions may carry
		// either value during migration, so return both for best-effort cleanup.
		if canonicalID := ws.ID(); canonicalID != id {
			removed = append(removed, canonicalID)
		}
	}
	return removed, errors.Join(errs...)
}

// PruneStale removes metadata that can no longer be reached from the project
// registry, old archived records, metadata for missing amux-managed roots, and
// lock files whose metadata no longer exists. The grace periods protect a
// concurrent project add or workspace create from a stale registry snapshot.
func (s *WorkspaceStore) PruneStale(options WorkspacePruneOptions) (WorkspacePruneResult, error) {
	var result WorkspacePruneResult
	if s == nil {
		return result, nil
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now()
	}
	registered := make(map[string]struct{}, len(options.RegisteredRepos))
	for _, repo := range options.RegisteredRepos {
		if canonical := canonicalLookupPath(repo); canonical != "" {
			registered[canonical] = struct{}{}
		}
	}

	ids, err := s.List()
	if err != nil {
		return result, err
	}
	var errs []error
	for _, id := range ids {
		reason, pruneErr := s.pruneWorkspaceIfStale(id, options, registered, now)
		if errors.Is(pruneErr, fs.ErrNotExist) {
			// Another process already reconciled this snapshot entry.
			continue
		}
		if pruneErr != nil {
			// Unreadable metadata is retained: without a trustworthy repo/root we
			// cannot prove it is stale.
			errs = append(errs, fmt.Errorf("prune workspace %s: %w", id, pruneErr))
			continue
		}
		if reason == "" {
			continue
		}
		switch reason {
		case "unregistered":
			result.UnregisteredRemoved++
		case "archived":
			result.ArchivedRemoved++
		case "missing_root":
			result.MissingRootRemoved++
		}
	}

	removedLocks, lockErr := s.pruneOrphanLocks()
	result.OrphanLocksRemoved = removedLocks
	if lockErr != nil {
		errs = append(errs, lockErr)
	}
	return result, errors.Join(errs...)
}

// pruneWorkspaceIfStale evaluates and deletes one record while holding its
// flock. A concurrent AddProject/rescan can therefore either refresh metadata
// before this check (which makes it fresh) or recreate it after deletion; an
// old pre-lock observation can never delete a newly refreshed record.
func (s *WorkspaceStore) pruneWorkspaceIfStale(
	id WorkspaceID,
	options WorkspacePruneOptions,
	registered map[string]struct{},
	now time.Time,
) (string, error) {
	locks, err := s.lockWorkspaceIDs(id)
	if err != nil {
		return "", err
	}
	defer unlockRegistryFiles(locks)

	ws, err := s.load(id, false)
	if err != nil {
		return "", err
	}
	modTime, err := s.metadataModTime(id)
	if err != nil {
		return "", err
	}

	reason := ""
	repo := canonicalLookupPath(ws.Repo)
	_, repoRegistered := registered[repo]
	switch {
	case repo != "" && !repoRegistered && oldEnough(modTime, now, options.OrphanGracePeriod):
		reason = "unregistered"
	case ws.Archived && oldEnough(archiveReferenceTime(ws, modTime), now, options.ArchivedRetention):
		reason = "archived"
	case repoRegistered && !ws.IsPrimaryCheckout() &&
		withinManagedRoot(options.ManagedRoot, ws.Root) &&
		pathMissing(ws.Root) && oldEnough(modTime, now, options.OrphanGracePeriod):
		reason = "missing_root"
	}
	if reason == "" {
		return "", nil
	}
	if err := s.deleteWorkspaceDir(id); err != nil {
		return "", fmt.Errorf("delete %s metadata: %w", reason, err)
	}
	s.removeWorkspaceLockFile(id)
	return reason, nil
}

func (s *WorkspaceStore) metadataModTime(id WorkspaceID) (time.Time, error) {
	info, err := os.Stat(s.workspacePath(id))
	if err == nil {
		return info.ModTime(), nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, err
	}
	backupInfo, backupErr := os.Stat(s.workspaceBackupPath(id))
	if backupErr != nil {
		return time.Time{}, backupErr
	}
	return backupInfo.ModTime(), nil
}

func (s *WorkspaceStore) pruneOrphanLocks() (int, error) {
	entries, err := os.ReadDir(s.root)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	removed := 0
	var errs []error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".lock") {
			continue
		}
		id := WorkspaceID(strings.TrimSuffix(entry.Name(), ".lock"))
		if validateErr := validateWorkspaceID(id); validateErr != nil {
			continue
		}
		locks, lockErr := s.lockWorkspaceIDs(id)
		if lockErr != nil {
			errs = append(errs, fmt.Errorf("lock orphan workspace %s: %w", id, lockErr))
			continue
		}
		if s.workspaceMetadataExists(id) {
			unlockRegistryFiles(locks)
			continue
		}
		s.removeWorkspaceLockFile(id)
		unlockRegistryFiles(locks)
		removed++
	}
	return removed, errors.Join(errs...)
}

func oldEnough(reference, now time.Time, grace time.Duration) bool {
	if reference.IsZero() || reference.After(now) {
		return false
	}
	if grace <= 0 {
		return true
	}
	return now.Sub(reference) >= grace
}

func archiveReferenceTime(ws *Workspace, fallback time.Time) time.Time {
	if ws != nil && !ws.ArchivedAt.IsZero() {
		return ws.ArchivedAt
	}
	return fallback
}

func withinManagedRoot(managedRoot, root string) bool {
	// Check the lexical pair first. On macOS, a missing child under /var cannot
	// be fully symlink-resolved to /private/var even though the existing managed
	// root can, so comparing only NormalizePath results would miss it.
	for _, pair := range [][2]string{
		{filepath.Clean(strings.TrimSpace(managedRoot)), filepath.Clean(strings.TrimSpace(root))},
		{canonicalLookupPath(managedRoot), canonicalLookupPath(root)},
	} {
		managed, target := pair[0], pair[1]
		if managed == "" || managed == "." || target == "" || target == "." {
			continue
		}
		rel, err := filepath.Rel(managed, target)
		if err != nil || rel == "." || rel == ".." {
			continue
		}
		if !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func pathMissing(path string) bool {
	_, err := os.Stat(path)
	return errors.Is(err, fs.ErrNotExist)
}
