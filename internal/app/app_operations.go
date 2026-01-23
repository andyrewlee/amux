package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
)

// loadProjects loads all registered projects and their worktrees
func (a *App) loadProjects() tea.Cmd {
	return func() tea.Msg {
		paths, err := a.registry.Projects()
		if err != nil {
			return messages.Error{Err: err, Context: "loading projects"}
		}

		var projects []data.Project
		for _, path := range paths {
			if !git.IsGitRepository(path) {
				continue
			}

			project := data.NewProject(path)
			worktrees, err := git.DiscoverWorktrees(project)
			if err != nil {
				continue
			}
			for i := range worktrees {
				wt := &worktrees[i]
				meta, err := a.metadata.Load(wt)
				if err != nil {
					logging.Warn("Failed to load metadata for %s: %v", wt.Root, err)
					continue
				}
				if meta.Base != "" {
					wt.Base = meta.Base
				}
				if meta.Created != "" {
					if createdAt, err := time.Parse(time.RFC3339, meta.Created); err == nil {
						wt.Created = createdAt
					} else if createdAt, err := time.Parse(time.RFC3339Nano, meta.Created); err == nil {
						wt.Created = createdAt
					} else {
						logging.Warn("Failed to parse worktree created time for %s: %v", wt.Root, err)
					}
				}
			}
			project.Worktrees = worktrees
			projects = append(projects, *project)
		}

		return messages.ProjectsLoaded{Projects: projects}
	}
}

// requestGitStatus requests git status for a worktree (always fetches fresh)
func (a *App) requestGitStatus(root string) tea.Cmd {
	return func() tea.Msg {
		status, err := git.GetStatus(root)
		// Update cache directly (no async refresh needed, we just fetched)
		if a.statusManager != nil && err == nil {
			a.statusManager.UpdateCache(root, status)
		}
		return messages.GitStatusResult{
			Root:   root,
			Status: status,
			Err:    err,
		}
	}
}

// requestGitStatusCached requests git status using cache if available
func (a *App) requestGitStatusCached(root string) tea.Cmd {
	// Check cache first
	if a.statusManager != nil {
		if cached := a.statusManager.GetCached(root); cached != nil {
			return func() tea.Msg {
				return messages.GitStatusResult{
					Root:   root,
					Status: cached,
					Err:    nil,
				}
			}
		}
	}
	// Cache miss, fetch fresh
	return a.requestGitStatus(root)
}

// addProject adds a new project to the registry
func (a *App) addProject(path string) tea.Cmd {
	return func() tea.Msg {
		logging.Info("Adding project: %s", path)

		// Expand path
		if len(path) > 0 && path[0] == '~' {
			home, err := os.UserHomeDir()
			if err == nil {
				path = filepath.Join(home, path[1:])
				logging.Debug("Expanded path to: %s", path)
			}
		}

		// Verify it's a git repo
		if !git.IsGitRepository(path) {
			logging.Warn("Path is not a git repository: %s", path)
			return messages.Error{
				Err:     fmt.Errorf("not a git repository: %s", path),
				Context: "adding project",
			}
		}

		// Add to registry
		if err := a.registry.AddProject(path); err != nil {
			logging.Error("Failed to add project to registry: %v", err)
			return messages.Error{Err: err, Context: "adding project"}
		}

		logging.Info("Project added successfully: %s", path)
		return messages.RefreshDashboard{}
	}
}

// createWorktree creates a new git worktree
func (a *App) createWorktree(project *data.Project, name, base string) tea.Cmd {
	return func() (msg tea.Msg) {
		var wt *data.Worktree
		defer func() {
			if r := recover(); r != nil {
				logging.Error("panic in createWorktree: %v", r)
				msg = messages.WorktreeCreateFailed{
					Worktree: wt,
					Err:      fmt.Errorf("create worktree panicked: %v", r),
				}
			}
		}()

		if project == nil || name == "" {
			return messages.WorktreeCreateFailed{
				Err: fmt.Errorf("missing project or worktree name"),
			}
		}

		worktreePath := filepath.Join(
			a.config.Paths.WorktreesRoot,
			project.Name,
			name,
		)

		branch := name
		wt = data.NewWorktree(name, branch, base, project.Path, worktreePath)

		if err := git.CreateWorktree(project.Path, worktreePath, branch, base); err != nil {
			return messages.WorktreeCreateFailed{
				Worktree: wt,
				Err:      err,
			}
		}

		// Wait for .git file to exist (race condition from git worktree add)
		gitPath := filepath.Join(worktreePath, ".git")
		for i := 0; i < 10; i++ {
			if _, err := os.Stat(gitPath); err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		meta := &data.Metadata{
			Name:       name,
			Branch:     branch,
			Repo:       project.Path,
			Base:       base,
			Created:    time.Now().Format(time.RFC3339),
			Assistant:  "claude",
			ScriptMode: "nonconcurrent",
			Env:        make(map[string]string),
		}

		if err := a.metadata.Save(wt, meta); err != nil {
			_ = git.RemoveWorktree(project.Path, worktreePath)
			_ = git.DeleteBranch(project.Path, branch)
			return messages.WorktreeCreateFailed{
				Worktree: wt,
				Err:      err,
			}
		}

		// Run setup scripts from .amux/worktrees.json
		if err := a.scripts.RunSetup(wt, meta); err != nil {
			// Don't fail worktree creation, just log the error
			return messages.WorktreeCreatedWithWarning{
				Worktree: wt,
				Warning:  fmt.Sprintf("setup failed: %v", err),
			}
		}

		return messages.WorktreeCreated{Worktree: wt}
	}
}

// deleteWorktree deletes a git worktree
func (a *App) deleteWorktree(project *data.Project, wt *data.Worktree) tea.Cmd {
	// Clear UI components if deleting the active worktree
	if a.activeWorktree != nil && a.activeWorktree.Root == wt.Root {
		a.goHome()
	}

	return func() tea.Msg {
		if wt.IsPrimaryCheckout() {
			return messages.WorktreeDeleteFailed{
				Project:  project,
				Worktree: wt,
				Err:      fmt.Errorf("cannot delete primary checkout"),
			}
		}

		if err := git.RemoveWorktree(project.Path, wt.Root); err != nil {
			return messages.WorktreeDeleteFailed{
				Project:  project,
				Worktree: wt,
				Err:      err,
			}
		}

		_ = git.DeleteBranch(project.Path, wt.Branch)
		_ = a.metadata.Delete(wt)

		return messages.WorktreeDeleted{
			Project:  project,
			Worktree: wt,
		}
	}
}

// removeProject removes a project from the registry (does not delete files).
func (a *App) removeProject(project *data.Project) tea.Cmd {
	if project == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("missing project"), Context: "removing project"}
		}
	}

	if a.activeWorktree != nil && a.activeWorktree.Repo == project.Path {
		a.goHome()
	}

	return func() tea.Msg {
		if err := a.registry.RemoveProject(project.Path); err != nil {
			return messages.Error{Err: err, Context: "removing project"}
		}
		return messages.ProjectRemoved{Path: project.Path}
	}
}
