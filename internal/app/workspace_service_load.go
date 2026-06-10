package app

import (
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
)

// LoadProjects loads all registered projects and their workspaces.
func (s *workspaceService) LoadProjects(loadToken projectsLoadToken) tea.Cmd {
	return func() tea.Msg {
		if s == nil || s.registry == nil {
			return messages.ProjectsLoaded{LoadToken: int(loadToken)}
		}
		paths, err := s.registry.Projects()
		if err != nil {
			return messages.Error{Err: err, Context: errorContext(errorServiceWorkspace, "loading projects")}
		}

		var projects []data.Project
		for _, path := range paths {
			if !git.IsGitRepository(path) {
				continue
			}

			project := data.NewProject(path)

			// Start from stored workspaces so metadata is authoritative.
			var storedWorkspaces []*data.Workspace
			if s.store != nil {
				storedWorkspaces, err = s.store.ListByRepo(path)
				if err != nil {
					logging.Warn("Failed to load stored workspaces for %s: %v", path, err)
				}
			}

			var workspaces []data.Workspace
			for _, ws := range storedWorkspaces {
				// Finish any delete that was tombstoned but interrupted before the
				// metadata was removed, instead of surfacing a dir-less ghost.
				if s.finishInterruptedDelete(ws) {
					continue
				}
				if !s.shouldSurfaceWorkspace(path, ws) {
					continue
				}
				workspaces = append(workspaces, *ws)
			}

			// Stored workspaces not discovered on disk are already included (store-first).
			// These may be workspaces whose directories were deleted.

			// Add primary checkout as transient workspace if not present
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
					// Skip creating primary workspace if we can't get the branch -
					// the repo may be in a bad state or no longer a valid git repo
				} else {
					primaryWs := data.NewWorkspace(
						filepath.Base(path), // name
						branch,              // branch
						"",                  // base
						path,                // repo
						path,                // root (same as repo for primary)
					)
					primaryWs.Assistant = s.resolvedDefaultAssistant()
					// Load any persisted UI state (OpenTabs, etc.) for the primary checkout
					if s.store != nil {
						found, loadErr := s.store.LoadMetadataFor(primaryWs)
						if loadErr != nil {
							logging.Warn("Failed to load metadata for primary checkout %s: %v", path, loadErr)
						} else if !found {
							// No stored metadata - save so UI state persists across restarts
							if err := s.store.Save(primaryWs); err != nil {
								logging.Warn("Failed to save primary checkout %s: %v", path, err)
							}
						}
					}
					workspaces = append([]data.Workspace{*primaryWs}, workspaces...)
				}
			}

			project.Workspaces = workspaces
			projects = append(projects, *project)
		}

		return messages.ProjectsLoaded{Projects: projects, LoadToken: int(loadToken)}
	}
}

// RescanWorkspaces discovers git worktrees and updates the workspace store.
func (s *workspaceService) RescanWorkspaces() tea.Cmd {
	return func() tea.Msg {
		if s == nil || s.registry == nil {
			return messages.RefreshDashboard{}
		}
		paths, err := s.registry.Projects()
		if err != nil {
			return messages.Error{Err: err, Context: errorContext(errorServiceWorkspace, "rescanning workspaces")}
		}

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
				if !s.shouldSurfaceWorkspace(path, ws) {
					continue
				}
				wsID := string(ws.ID())
				discoveredSet[wsID] = true
				if s.isDeleteInFlight(wsID) {
					// A workspace being deleted must not be re-imported by a
					// concurrent rescan racing the delete; the delete flow owns it.
					continue
				}
				// Set the default assistant for newly discovered workspaces. Note:
				// UpsertFromDiscovery below merges with stored metadata, where stored
				// metadata takes precedence if non-empty. This is intentional — stored
				// metadata is authoritative over the discovery default.
				if strings.TrimSpace(ws.Assistant) == "" {
					ws.Assistant = s.resolvedDefaultAssistant()
				}
				if s.store != nil {
					var upsertErr error
					imported := s.runUnlessDeleteInFlight(wsID, func() {
						upsertErr = s.store.UpsertFromDiscovery(ws)
					})
					if !imported {
						continue
					}
					if upsertErr != nil {
						logging.Warn("Failed to import workspace %s: %v", ws.Name, upsertErr)
					}
				}
			}

			var storedWorkspaces []*data.Workspace
			if s.store != nil {
				storedWorkspaces, err = s.store.ListByRepoIncludingArchived(path)
				if err != nil {
					logging.Warn("Failed to load stored workspaces for %s: %v", path, err)
					continue
				}
			}

			for _, ws := range storedWorkspaces {
				if ws == nil {
					continue
				}
				if s.isDeleteInFlight(string(ws.ID())) {
					// Leave a workspace mid-delete untouched: neither archival Save
					// path below should run while the delete flow is removing it.
					continue
				}
				if !s.shouldSurfaceWorkspace(path, ws) {
					if !ws.Archived {
						var saveErr error
						saved := s.runUnlessDeleteInFlight(string(ws.ID()), func() {
							ws.Archived = true
							ws.ArchivedAt = time.Now()
							if s.store != nil {
								saveErr = s.store.Save(ws)
							}
						})
						if !saved {
							continue
						}
						if saveErr != nil {
							logging.Warn("Failed to archive unmanaged workspace %s: %v", ws.Name, saveErr)
						}
					}
					continue
				}
				if discoveredSet[string(ws.ID())] {
					continue
				}
				if !ws.Archived {
					var saveErr error
					saved := s.runUnlessDeleteInFlight(string(ws.ID()), func() {
						ws.Archived = true
						ws.ArchivedAt = time.Now()
						if s.store != nil {
							saveErr = s.store.Save(ws)
						}
					})
					if !saved {
						continue
					}
					if saveErr != nil {
						logging.Warn("Failed to archive workspace %s: %v", ws.Name, saveErr)
					}
				}
			}
		}

		return messages.RefreshDashboard{}
	}
}
