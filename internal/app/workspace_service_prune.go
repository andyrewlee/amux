package app

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
)

type workspaceMetadataPruner interface {
	PruneStale(data.WorkspacePruneOptions) (data.WorkspacePruneResult, error)
}

type workspaceRepoMetadataDeleter interface {
	DeleteByRepo(repoPath string) ([]data.WorkspaceID, error)
}

func (s *workspaceService) reconcileWorkspaceMetadata(registeredRepos []string) {
	if s == nil || s.store == nil {
		return
	}
	pruner, ok := s.store.(workspaceMetadataPruner)
	if !ok {
		return
	}
	result, err := pruner.PruneStale(data.WorkspacePruneOptions{
		RegisteredRepos:   registeredRepos,
		ManagedRoot:       s.workspacesRoot,
		Now:               time.Now(),
		OrphanGracePeriod: metadataOrphanGracePeriod,
		ArchivedRetention: archivedMetadataRetention,
	})
	if err != nil {
		logging.Warn("workspace metadata reconciliation completed with errors: %v", err)
	}
	if result.MetadataRemoved() > 0 || result.OrphanLocksRemoved > 0 {
		logging.Info(
			"workspace metadata reconciliation: removed=%d unregistered=%d missing_root=%d archived=%d orphan_locks=%d",
			result.MetadataRemoved(),
			result.UnregisteredRemoved,
			result.MissingRootRemoved,
			result.ArchivedRemoved,
			result.OrphanLocksRemoved,
		)
	}
}

// pruneMissingTemporaryProjects forgets vanished repositories rooted in the OS
// temp directory. A missing arbitrary path may be an offline volume and is
// retained; a vanished temp directory cannot come back with the same contents.
func (s *workspaceService) pruneMissingTemporaryProjects(paths []string) []string {
	if s == nil || s.registry == nil {
		return paths
	}
	kept := make([]string, 0, len(paths))
	for _, path := range paths {
		_, statErr := os.Stat(path)
		if !errors.Is(statErr, fs.ErrNotExist) || !isTemporaryProjectPath(path) {
			kept = append(kept, path)
			continue
		}
		if err := s.registry.RemoveProject(path); err != nil {
			logging.Warn("Failed to remove vanished temporary project %s: %v", path, err)
			kept = append(kept, path)
			continue
		}
		s.removeProjectMetadata(path)
		logging.Info("Removed vanished temporary project from registry: %s", path)
	}
	return kept
}

func isTemporaryProjectPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	tempRoots := []string{os.TempDir()}
	if os.TempDir() != "/tmp" {
		tempRoots = append(tempRoots, "/tmp")
	}
	for _, root := range tempRoots {
		if pathWithinAliasesStrict(workspacePathAliases(root), workspacePathAliases(path)) {
			return true
		}
	}
	return false
}

func (s *workspaceService) removeProjectMetadata(repoPath string, knownWorkspaces ...data.Workspace) {
	if s == nil {
		return
	}
	idsToKill := make(map[string]struct{}, len(knownWorkspaces))
	for i := range knownWorkspaces {
		if id := string(knownWorkspaces[i].ID()); id != "" {
			idsToKill[id] = struct{}{}
		}
	}
	killCollectedSessions := func() {
		for id := range idsToKill {
			s.killWorkspaceSessionsForDelete(id)
		}
	}
	if s.store == nil {
		killCollectedSessions()
		return
	}
	workspaces, listErr := s.store.ListByRepoIncludingArchived(repoPath)
	if listErr != nil {
		logging.Warn("Project removed, but workloads could not be fully enumerated for %s: %v", repoPath, listErr)
	}
	for _, ws := range workspaces {
		if ws == nil || s.scripts == nil {
			continue
		}
		if err := s.scripts.Stop(ws); err != nil {
			logging.Warn("Project removed, but scripts could not be stopped for workspace %s: %v", ws.Name, err)
		}
		s.scripts.ReleaseWorkspace(ws)
	}
	if deleter, ok := s.store.(workspaceRepoMetadataDeleter); ok {
		ids, err := deleter.DeleteByRepo(repoPath)
		if err != nil {
			logging.Warn("Project removed, but some metadata could not be cleaned for %s: %v", repoPath, err)
		}
		for _, id := range ids {
			idsToKill[string(id)] = struct{}{}
		}
		killCollectedSessions()
		s.restoreMetadataIfProjectReadded(repoPath)
		return
	}

	// Compatibility path for test/custom stores implementing only WorkspaceStore.
	if listErr != nil {
		logging.Warn("Project removed, but metadata could not be listed for %s: %v", repoPath, listErr)
		killCollectedSessions()
		return
	}
	for _, ws := range workspaces {
		if ws == nil {
			continue
		}
		id := ws.ID()
		if err := s.store.Delete(id); err != nil {
			logging.Warn("Project removed, but workspace metadata %s could not be cleaned: %v", id, err)
			continue
		}
		idsToKill[string(id)] = struct{}{}
	}
	killCollectedSessions()
	s.restoreMetadataIfProjectReadded(repoPath)
}

func (s *workspaceService) restoreMetadataIfProjectReadded(repoPath string) {
	if s == nil || s.registry == nil {
		return
	}
	paths, err := s.registry.Projects()
	if err != nil {
		logging.Warn("Could not verify whether project %s was re-added during metadata cleanup: %v", repoPath, err)
		return
	}
	target := data.NormalizePath(repoPath)
	for _, path := range paths {
		if data.NormalizePath(path) != target {
			continue
		}
		// Registry removal happened before metadata cleanup. If the project is
		// present again now, another process re-added it later and therefore wins.
		s.importManagedWorkspaces(path)
		return
	}
}
