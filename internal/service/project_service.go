package service

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/andyrewlee/medusa/internal/config"
	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/git"
	"github.com/andyrewlee/medusa/internal/logging"
)

// ProjectService manages registered git repositories and their workspaces.
type ProjectService struct {
	registry   *data.Registry
	workspaces *data.WorkspaceStore
	config     *config.Config
	eventBus   *EventBus
}

// NewProjectService creates a project service.
func NewProjectService(registry *data.Registry, workspaces *data.WorkspaceStore, cfg *config.Config, bus *EventBus) *ProjectService {
	return &ProjectService{
		registry:   registry,
		workspaces: workspaces,
		config:     cfg,
		eventBus:   bus,
	}
}

// ListProjects returns all registered projects with their workspaces loaded.
func (s *ProjectService) ListProjects() ([]data.Project, error) {
	s.adoptOrphanedWorkspaces()

	regProjects, err := s.registry.LoadFull()
	if err != nil {
		return nil, fmt.Errorf("loading projects: %w", err)
	}

	groupRoots := s.groupOwnedRoots()
	var projects []data.Project

	for _, rp := range regProjects {
		path := rp.Path
		if !git.IsGitRepository(path) {
			continue
		}

		project := data.NewProject(path)
		project.Profile = rp.Profile

		storedWorkspaces, err := s.workspaces.ListByRepo(path)
		if err != nil {
			logging.Warn("Failed to load stored workspaces for %s: %v", path, err)
		}

		// Legacy import check
		needsLegacyImport, err := s.workspaces.HasLegacyWorkspaces(path)
		if err != nil {
			logging.Warn("Failed to check legacy workspaces for %s: %v", path, err)
		}
		if needsLegacyImport {
			discoveredWorkspaces, err := git.DiscoverWorkspaces(project)
			if err != nil {
				logging.Warn("Failed to discover workspaces for %s: %v", path, err)
			} else {
				for i := range discoveredWorkspaces {
					ws := &discoveredWorkspaces[i]
					if groupRoots[data.NormalizePath(ws.Root)] {
						continue
					}
					if err := s.workspaces.UpsertFromDiscoveryPreserveArchived(ws); err != nil {
						logging.Warn("Failed to import workspace %s: %v", ws.Name, err)
					}
				}
				storedWorkspaces, err = s.workspaces.ListByRepo(path)
				if err != nil {
					logging.Warn("Failed to reload stored workspaces for %s: %v", path, err)
				}
			}
		}

		var workspaces []data.Workspace
		for _, ws := range storedWorkspaces {
			if groupRoots[data.NormalizePath(ws.Root)] {
				continue
			}
			workspaces = append(workspaces, *ws)
		}

		// Add primary checkout if not present
		hasPrimary := false
		for _, ws := range workspaces {
			if ws.IsPrimaryCheckout() {
				hasPrimary = true
				break
			}
		}

		if !hasPrimary {
			branch, err := git.GetCurrentBranch(path)
			if err != nil {
				logging.Warn("Failed to get current branch for %s: %v", path, err)
			} else {
				primaryWs := data.NewWorkspace(filepath.Base(path), branch, "", path, path)
				found, loadErr := s.workspaces.LoadMetadataFor(primaryWs)
				if loadErr != nil {
					logging.Warn("Failed to load metadata for primary checkout %s: %v", path, loadErr)
				} else if !found {
					if err := s.workspaces.Save(primaryWs); err != nil {
						logging.Warn("Failed to save primary checkout %s: %v", path, err)
					}
				}
				workspaces = append([]data.Workspace{*primaryWs}, workspaces...)
			}
		}

		// Propagate project profile
		for i := range workspaces {
			workspaces[i].Profile = project.Profile
		}
		project.Workspaces = workspaces
		projects = append(projects, *project)
	}

	s.eventBus.Publish(NewEvent(EventProjectsLoaded, nil))
	return projects, nil
}

// AddProject registers a new git repository.
func (s *ProjectService) AddProject(path string) error {
	// Expand ~
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[1:])
		}
	}

	if !git.IsGitRepository(path) {
		return fmt.Errorf("not a git repository: %s", path)
	}

	if err := s.registry.AddProject(path); err != nil {
		return fmt.Errorf("adding project: %w", err)
	}

	s.eventBus.Publish(NewEvent(EventProjectAdded, map[string]string{"path": path}))
	return nil
}

// RemoveProject unregisters a project (does not delete files).
func (s *ProjectService) RemoveProject(path string) error {
	if err := s.registry.RemoveProject(path); err != nil {
		return fmt.Errorf("removing project: %w", err)
	}

	// Clean up empty workspace directory
	_ = os.Remove(filepath.Join(s.config.Paths.WorkspacesRoot, filepath.Base(path)))

	s.eventBus.Publish(NewEvent(EventProjectRemoved, map[string]string{"path": path}))
	return nil
}

// SetProfile sets the Claude profile for a project.
func (s *ProjectService) SetProfile(path, profile string) error {
	return s.registry.SetProfile(path, profile)
}

// RescanWorkspaces rediscovers git worktrees for all registered projects.
func (s *ProjectService) RescanWorkspaces() error {
	paths, err := s.registry.Projects()
	if err != nil {
		return fmt.Errorf("listing projects: %w", err)
	}

	groupRoots := s.groupOwnedRoots()

	for _, path := range paths {
		if !git.IsGitRepository(path) {
			continue
		}

		project := data.NewProject(path)
		discoveredWorkspaces, err := git.DiscoverWorkspaces(project)
		if err != nil {
			logging.Warn("Failed to discover workspaces for %s: %v", path, err)
			continue
		}

		discoveredSet := make(map[string]bool, len(discoveredWorkspaces))
		for i := range discoveredWorkspaces {
			ws := &discoveredWorkspaces[i]
			if groupRoots[data.NormalizePath(ws.Root)] {
				continue
			}
			discoveredSet[string(ws.ID())] = true
			if err := s.workspaces.UpsertFromDiscovery(ws); err != nil {
				logging.Warn("Failed to import workspace %s: %v", ws.Name, err)
			}
		}
	}

	s.eventBus.Publish(NewEvent(EventProjectRescan, nil))
	return nil
}

// GetGitStatus returns the git status for a workspace root directory.
func (s *ProjectService) GetGitStatus(root string) (*git.StatusResult, error) {
	return git.GetStatus(root)
}

// --- internal helpers ---

func (s *ProjectService) groupOwnedRoots() map[string]bool {
	roots := make(map[string]bool)
	groups, err := s.registry.LoadGroups()
	if err != nil {
		return roots
	}
	for _, group := range groups {
		gwList, err := s.workspaces.ListGroupWorkspacesByGroup(group.Name)
		if err != nil {
			continue
		}
		for _, gw := range gwList {
			for _, root := range gw.AllRoots() {
				roots[data.NormalizePath(root)] = true
			}
		}
	}
	return roots
}

func (s *ProjectService) adoptOrphanedWorkspaces() {
	if s.config == nil || s.config.Paths == nil || s.config.Paths.WorkspacesRoot == "" {
		return
	}

	regProjects, err := s.registry.LoadFull()
	if err != nil {
		logging.Warn("Failed to load registry for orphan scan: %v", err)
		return
	}

	registeredNames := make(map[string]bool, len(regProjects))
	registeredPaths := make(map[string]bool, len(regProjects))
	for _, rp := range regProjects {
		registeredNames[filepath.Base(rp.Path)] = true
		registeredPaths[data.NormalizePath(rp.Path)] = true
	}

	entries, err := os.ReadDir(s.config.Paths.WorkspacesRoot)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "groups" {
			continue
		}
		if registeredNames[entry.Name()] {
			continue
		}

		projectDir := filepath.Join(s.config.Paths.WorkspacesRoot, entry.Name())
		repoPath := resolveOrphanedRepoService(projectDir)
		if repoPath == "" {
			continue
		}
		if registeredPaths[data.NormalizePath(repoPath)] {
			continue
		}

		logging.Info("Auto-adopting orphaned workspace dir %s -> repo %s", entry.Name(), repoPath)
		if err := s.registry.AddProject(repoPath); err != nil {
			logging.Warn("Failed to auto-register orphaned project %s: %v", repoPath, err)
		}
	}
}

func resolveOrphanedRepoService(projectDir string) string {
	subEntries, err := os.ReadDir(projectDir)
	if err != nil {
		return ""
	}
	for _, sub := range subEntries {
		if !sub.IsDir() {
			continue
		}
		worktreePath := filepath.Join(projectDir, sub.Name())
		resolved, err := git.ResolveWorktreeRepo(worktreePath)
		if err != nil {
			continue
		}
		if git.IsGitRepository(resolved) {
			return resolved
		}
	}
	return ""
}
