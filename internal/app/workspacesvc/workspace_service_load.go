package workspacesvc

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

// workspaceLookupKey is the canonical (repo, root) pair key — the same
// normalization the store's findStoredWorkspace matcher uses
// (canonicalLookupPath: TrimSpace + NormalizePath). Rescan membership keyed
// this way is immune to storeID drift, unlike an ID() comparison.
func workspaceLookupKey(repo, root string) string {
	return data.NormalizePath(strings.TrimSpace(repo)) + "\n" + data.NormalizePath(strings.TrimSpace(root))
}

// recordSetLister is the snapshot capability a real WorkspaceStore provides:
// one List+Load round shared across every project's ByRepo filter, instead of
// a full metadata re-read per project per reload. Fake stores that only
// implement the WorkspaceStore interface fall back to per-repo calls.
type recordSetLister interface {
	ListAll() (*data.WorkspaceRecordSet, error)
}

// listByRepoFromSet filters workspaces for one repo from the shared snapshot
// when available, else via the store's per-repo path — identical semantics.
func (s *Service) listByRepoFromSet(set *data.WorkspaceRecordSet, repoPath string, includeArchived bool) ([]*data.Workspace, error) {
	if set != nil {
		return set.ByRepo(repoPath, includeArchived)
	}
	if includeArchived {
		return s.store.ListByRepoIncludingArchived(repoPath)
	}
	return s.store.ListByRepo(repoPath)
}

func (s *Service) loadWorkspaceRecordSet() *data.WorkspaceRecordSet {
	if s.store == nil {
		return nil
	}
	lister, ok := s.store.(recordSetLister)
	if !ok {
		return nil
	}
	set, err := lister.ListAll()
	if err != nil {
		logging.Warn("Failed to snapshot workspace metadata; falling back to per-repo reads: %v", err)
		return nil
	}
	return set
}

// LoadProjects loads all registered projects and their workspaces.
func (s *Service) LoadProjects(loadToken int) tea.Cmd {
	return func() tea.Msg {
		if s == nil || s.registry == nil {
			return messages.ProjectsLoaded{LoadToken: loadToken}
		}
		paths, err := s.registry.Projects()
		if err != nil {
			return messages.Error{Err: err, Context: workspaceErrContext("loading projects")}
		}
		paths = s.pruneMissingTemporaryProjects(paths)
		// Metadata reconciliation scans every persisted record. Run it for the
		// initial dashboard load rather than on each UI refresh; explicit project
		// and workspace removals clean their own state immediately.
		if loadToken <= 1 {
			s.reconcileWorkspaceMetadata(paths)
		}
		// One metadata List+Load round serves every project's filter below —
		// previously each project re-read every record (O(projects × records)
		// JSON reads per reload).
		recordSet := s.loadWorkspaceRecordSet()

		var projects []data.Project
		for _, path := range paths {
			if !git.IsGitRepository(path) {
				continue
			}

			project := data.NewProject(path)

			// Start from stored workspaces so metadata is authoritative.
			var storedWorkspaces []*data.Workspace
			if s.store != nil {
				storedWorkspaces, err = s.listByRepoFromSet(recordSet, path, false)
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
				workspaces = s.prependPrimaryCheckout(path, workspaces)
			}

			project.Workspaces = workspaces
			// Shelf entries ride alongside, not inside, Workspaces — the
			// sidebar tree and every live-set consumer must never see them.
			project.ShelvedWorkspaces = s.listShelvedWorkspaces(path)
			projects = append(projects, *project)
		}

		return messages.ProjectsLoaded{Projects: projects, LoadToken: loadToken}
	}
}

// importManagedWorkspaces discovers and persists amux-owned worktrees for one
// project. It is best-effort because registration has already succeeded; a
// transient git/store error should not roll back a valid project addition.
func (s *Service) importManagedWorkspaces(path string) {
	if s == nil || s.store == nil || s.gitOps == nil || !git.IsGitRepository(path) {
		return
	}
	project := data.NewProject(path)
	discovered, err := s.gitOps.DiscoverWorkspaces(project)
	if err != nil {
		logging.Warn("Failed to discover workspaces while adding %s: %v", path, err)
		return
	}
	for i := range discovered {
		ws := &discovered[i]
		if !s.shouldSurfaceWorkspace(path, ws) {
			continue
		}
		if strings.TrimSpace(ws.Assistant) == "" {
			ws.Assistant = s.resolvedDefaultAssistant()
		}
		var upsertErr error
		if !s.runUnlessMutationInFlight(ws, func() {
			upsertErr = s.store.UpsertFromDiscovery(ws)
		}) {
			continue
		}
		if upsertErr != nil {
			logging.Warn("Failed to import workspace %s while adding %s: %v", ws.Name, path, upsertErr)
		}
	}
}

// prependPrimaryCheckout inserts the repo's primary checkout as a transient
// workspace at the front of workspaces, loading any persisted UI state. It
// returns workspaces unchanged when the repo's branch can't be read — the
// repo may be in a bad state or no longer a valid git repo.
func (s *Service) prependPrimaryCheckout(path string, workspaces []data.Workspace) []data.Workspace {
	branch, err := git.GetCurrentBranch(path)
	if err != nil {
		logging.Warn("Failed to get current branch for %s: %v", path, err)
		return workspaces
	}
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
	return append([]data.Workspace{*primaryWs}, workspaces...)
}

// RescanWorkspaces discovers git worktrees and updates the workspace store.
func (s *Service) RescanWorkspaces() tea.Cmd {
	return func() tea.Msg {
		if s == nil || s.registry == nil {
			return messages.RefreshDashboard{}
		}
		paths, err := s.registry.Projects()
		if err != nil {
			return messages.Error{Err: err, Context: workspaceErrContext("rescanning workspaces")}
		}

		// One snapshot serves every project's stored-workspace pass below; see
		// LoadProjects for why per-repo ListByRepo calls don't scale.
		recordSet := s.loadWorkspaceRecordSet()

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

			// discoveredSet keys on the canonical (repo, root) pair — the same
			// normalization findStoredWorkspace matches on — so a stored record
			// with a drifted storeID still matches its discovered worktree and
			// isn't flap-archived. The mutation probes below take the workspace
			// itself — every ID form drifts with path existence mid-mutation.
			discoveredSet := make(map[string]bool, len(discoveredWorkspaces))
			for i := range discoveredWorkspaces {
				ws := &discoveredWorkspaces[i]
				if !s.shouldSurfaceWorkspace(path, ws) {
					continue
				}
				discoveredSet[workspaceLookupKey(ws.Repo, ws.Root)] = true
				if s.isMutationInFlight(ws) {
					// A workspace being deleted must not be re-imported by a
					// concurrent rescan racing the delete; the delete flow owns it.
					continue
				}
				if s.hasDeleteTombstone(ws) {
					// A durable delete tombstone on a discovered path means the
					// delete hasn't finished reaping — re-importing now would
					// resurrect the row the delete flow is still owning.
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
					imported := s.runUnlessMutationInFlight(ws, func() {
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
				storedWorkspaces, err = s.listByRepoFromSet(recordSet, path, true)
				if err != nil {
					logging.Warn("Failed to load stored workspaces for %s: %v", path, err)
					continue
				}
			}

			for _, ws := range storedWorkspaces {
				if ws == nil {
					continue
				}
				if s.isMutationInFlight(ws) {
					// Leave a workspace mid-mutation untouched: neither archival Save
					// path below should run while the delete flow is removing it.
					continue
				}
				if s.hasDeleteTombstone(ws) {
					// A durable delete tombstone means recovery hasn't finished.
					// The worktree may be gone (finishInterruptedDelete surfaced
					// it), but archiving here would exclude it from listByRepo and
					// permanently break the recovery loop.
					continue
				}
				if !s.shouldSurfaceWorkspace(path, ws) {
					s.archiveWorkspaceRecord(ws, "unmanaged workspace")
					continue
				}
				if discoveredSet[workspaceLookupKey(ws.Repo, ws.Root)] {
					continue
				}
				s.archiveWorkspaceRecord(ws, "workspace")
			}
		}

		return messages.RefreshDashboard{}
	}
}

// archiveWorkspaceRecord marks ws archived and persists it under the mutation
// guard — the shared tail of the rescan's two stored-workspace archival paths.
// kind is the log label ("workspace" or "unmanaged workspace"). Returns false
// when the mutation guard rejected the write; the workspace is left untouched.
func (s *Service) archiveWorkspaceRecord(ws *data.Workspace, kind string) bool {
	if ws.Archived {
		return true
	}
	var saveErr error
	saved := s.runUnlessMutationInFlight(ws, func() {
		ws.Archived = true
		ws.ArchivedAt = time.Now()
		if s.store != nil {
			saveErr = s.store.Save(ws)
		}
	})
	if !saved {
		return false
	}
	if saveErr != nil {
		logging.Warn("Failed to archive %s %s: %v", kind, ws.Name, saveErr)
	}
	return true
}
