package app

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/git"
	"github.com/andyrewlee/medusa/internal/logging"
	"github.com/andyrewlee/medusa/internal/messages"
)

// adoptOrphanedGroupWorkspaces scans the groups workspace directory for directories
// not associated with any registered group. For each orphan, it resolves the
// repos from worktree subdirectories and auto-registers a group.
func (a *App) adoptOrphanedGroupWorkspaces() {
	if a.config == nil || a.config.Paths == nil || a.config.Paths.GroupsWorkspacesRoot == "" {
		return
	}

	groups, err := a.registry.LoadGroups()
	if err != nil {
		logging.Warn("Failed to load groups for orphan scan: %v", err)
		return
	}

	registeredGroupNames := make(map[string]bool, len(groups))
	for _, g := range groups {
		registeredGroupNames[g.Name] = true
	}

	entries, err := os.ReadDir(a.config.Paths.GroupsWorkspacesRoot)
	if err != nil {
		if !os.IsNotExist(err) {
			logging.Warn("Failed to read groups root for orphan scan: %v", err)
		}
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if registeredGroupNames[entry.Name()] {
			continue
		}

		groupDir := filepath.Join(a.config.Paths.GroupsWorkspacesRoot, entry.Name())
		repos := resolveOrphanedGroupRepos(groupDir)
		if len(repos) == 0 {
			logging.Warn("Orphaned group dir %s: no valid repos found, skipping", entry.Name())
			continue
		}

		logging.Info("Auto-adopting orphaned group dir %s with %d repos", entry.Name(), len(repos))
		if err := a.registry.AddGroup(entry.Name(), repos, ""); err != nil {
			logging.Warn("Failed to auto-register orphaned group %s: %v", entry.Name(), err)
		} else {
			registeredGroupNames[entry.Name()] = true
		}
	}
}

// resolveOrphanedGroupRepos scans a group directory for workspace subdirectories
// containing repo worktrees, and resolves them back to original repo paths.
// Expected layout: groups/<group-name>/<workspace-name>/<repo-name>/
func resolveOrphanedGroupRepos(groupDir string) []data.GroupRepo {
	wsEntries, err := os.ReadDir(groupDir)
	if err != nil {
		return nil
	}

	repoPathSet := make(map[string]bool)
	var repos []data.GroupRepo

	for _, wsEntry := range wsEntries {
		if !wsEntry.IsDir() {
			continue
		}
		wsDir := filepath.Join(groupDir, wsEntry.Name())

		repoEntries, err := os.ReadDir(wsDir)
		if err != nil {
			continue
		}

		for _, repoEntry := range repoEntries {
			if !repoEntry.IsDir() {
				continue
			}
			worktreePath := filepath.Join(wsDir, repoEntry.Name())
			resolved, err := git.ResolveWorktreeRepo(worktreePath)
			if err != nil || !git.IsGitRepository(resolved) {
				continue
			}
			normalized := data.NormalizePath(resolved)
			if !repoPathSet[normalized] {
				repoPathSet[normalized] = true
				repos = append(repos, data.GroupRepo{
					Path: resolved,
					Name: filepath.Base(resolved),
				})
			}
		}

		// Once we've found repos from one workspace, that's enough —
		// all workspaces in a group reference the same repos.
		if len(repos) > 0 {
			break
		}
	}

	return repos
}

// loadGroups loads all project groups and their workspaces from the store.
func (a *App) loadGroups() tea.Cmd {
	return func() tea.Msg {
		a.adoptOrphanedGroupWorkspaces()

		groups, err := a.registry.LoadGroups()
		if err != nil {
			logging.Warn("Failed to load groups: %v", err)
			return messages.GroupsLoaded{Groups: nil}
		}

		for i := range groups {
			ws, err := a.workspaces.ListGroupWorkspacesByGroup(groups[i].Name)
			if err != nil {
				logging.Warn("Failed to load group workspaces for %s: %v", groups[i].Name, err)
				continue
			}
			for _, gw := range ws {
				gw.Profile = groups[i].Profile
				// Propagate profile to inner workspaces so agent launch doesn't re-prompt
				gw.Primary.Profile = groups[i].Profile
				for j := range gw.Secondary {
					gw.Secondary[j].Profile = groups[i].Profile
				}
				// Merge persisted tab state from the Primary workspace's store entry.
				// Tab persistence writes to workspace.json keyed by Primary.ID(),
				// which is a different path than group_workspace.json keyed by GroupWorkspace.ID().
				if stored, err := a.workspaces.Load(gw.Primary.ID()); err == nil && stored != nil {
					gw.Primary.OpenTabs = stored.OpenTabs
					gw.Primary.ActiveTabIndex = stored.ActiveTabIndex
				}
				groups[i].Workspaces = append(groups[i].Workspaces, *gw)
			}
		}

		return messages.GroupsLoaded{Groups: groups}
	}
}

// createGroup registers repos and adds a group to the registry.
func (a *App) createGroup(name string, repoPaths []string, profile string) tea.Cmd {
	return func() tea.Msg {
		repos := make([]data.GroupRepo, len(repoPaths))
		for i, p := range repoPaths {
			repos[i] = data.GroupRepo{
				Path: p,
				Name: filepath.Base(p),
			}
		}

		if err := a.registry.AddGroup(name, repos, profile); err != nil {
			logging.Error("Failed to create group: %v", err)
			return messages.Error{Err: err, Context: "creating group"}
		}

		return messages.GroupCreated{Name: name}
	}
}

// removeGroup deletes all group workspaces and removes the group from registry.
func (a *App) removeGroup(name string) tea.Cmd {
	groupsRoot := a.config.Paths.GroupsWorkspacesRoot
	return func() tea.Msg {
		// Find the group
		var group *data.ProjectGroup
		for i := range a.groups {
			if a.groups[i].Name == name {
				group = &a.groups[i]
				break
			}
		}

		if group != nil {
			// Delete all group workspaces first
			for i := range group.Workspaces {
				gw := &group.Workspaces[i]
				deleteGroupWorkspaceSync(a, group, gw)
			}
		}

		if err := a.registry.RemoveGroup(name); err != nil {
			logging.Error("Failed to remove group: %v", err)
			return messages.Error{Err: err, Context: "removing group"}
		}

		// Clean up group workspace directory (and any leftover subdirs)
		groupDir := filepath.Join(groupsRoot, name)
		_ = os.RemoveAll(groupDir)

		return messages.GroupRemoved{Name: name}
	}
}

// fetchFirstGroupBase validates the branch and fetches the first repo's remote base.
// Subsequent repos are fetched one at a time via fetchNextGroupBase.
func (a *App) fetchFirstGroupBase(group *data.ProjectGroup, name string, allowEdits, loadClaudeMD bool) tea.Cmd {
	groupsRoot := a.config.Paths.GroupsWorkspacesRoot
	repos := make([]data.GroupRepo, len(group.Repos))
	copy(repos, group.Repos)
	return func() tea.Msg {
		if group == nil || name == "" {
			return messages.GroupWorkspaceCreateFailed{
				Err: fmt.Errorf("missing group or workspace name"),
			}
		}

		// Validate branch doesn't exist in any repo
		if err := git.ValidateBranchAcrossRepos(name, group.RepoPaths()); err != nil {
			return messages.GroupWorkspaceCreateFailed{Err: err}
		}

		// Fetch first repo
		repo := repos[0]
		base, err := git.GetFreshRemoteBase(repo.Path)
		if err != nil {
			return messages.GroupWorkspaceCreateFailed{
				Err: fmt.Errorf("failed to get base for %s: %w", repo.Name, err),
			}
		}

		// Verify the base ref resolves to a commit (catches empty repos)
		if err := git.ValidateRef(repo.Path, base); err != nil {
			return messages.GroupWorkspaceCreateFailed{
				Err: fmt.Errorf("%s: repo has no commits on %q", repo.Name, base),
			}
		}

		wsPath := filepath.Join(groupsRoot, group.Name, name, repo.Name)
		spec := git.RepoSpec{
			RepoPath:      repo.Path,
			RepoName:      repo.Name,
			WorkspacePath: wsPath,
			Branch:        name,
			Base:          base,
		}

		return messages.GroupRepoFetchDone{
			Group:          group,
			Name:           name,
			FetchedSpecs:   []git.RepoSpec{spec},
			RemainingRepos: repos[1:],
			AllowEdits:     allowEdits,
			LoadClaudeMD:   loadClaudeMD,
		}
	}
}

// fetchNextGroupBase fetches the remote base for the next repo in the chain.
func (a *App) fetchNextGroupBase(group *data.ProjectGroup, name string, specs []git.RepoSpec, remaining []data.GroupRepo, allowEdits, loadClaudeMD bool) tea.Cmd {
	groupsRoot := a.config.Paths.GroupsWorkspacesRoot
	repo := remaining[0]
	rest := make([]data.GroupRepo, len(remaining)-1)
	copy(rest, remaining[1:])
	return func() tea.Msg {
		base, err := git.GetFreshRemoteBase(repo.Path)
		if err != nil {
			return messages.GroupWorkspaceCreateFailed{
				Err: fmt.Errorf("failed to get base for %s: %w", repo.Name, err),
			}
		}

		// Verify the base ref resolves to a commit (catches empty repos)
		if err := git.ValidateRef(repo.Path, base); err != nil {
			return messages.GroupWorkspaceCreateFailed{
				Err: fmt.Errorf("%s: repo has no commits on %q", repo.Name, base),
			}
		}

		wsPath := filepath.Join(groupsRoot, group.Name, name, repo.Name)
		newSpecs := append(specs, git.RepoSpec{
			RepoPath:      repo.Path,
			RepoName:      repo.Name,
			WorkspacePath: wsPath,
			Branch:        name,
			Base:          base,
		})

		return messages.GroupRepoFetchDone{
			Group:          group,
			Name:           name,
			FetchedSpecs:   newSpecs,
			RemainingRepos: rest,
			AllowEdits:     allowEdits,
			LoadClaudeMD:   loadClaudeMD,
		}
	}
}

// handleGroupRepoFetchDone handles per-repo fetch completion during group workspace creation.
func (a *App) handleGroupRepoFetchDone(msg messages.GroupRepoFetchDone) []tea.Cmd {
	var cmds []tea.Cmd
	if len(msg.RemainingRepos) > 0 {
		// Update detail to next repo being fetched
		if a.creationOverlay != nil {
			a.creationOverlay.SetStepDetail(msg.RemainingRepos[0].Name)
		}
		cmds = append(cmds, a.fetchNextGroupBase(msg.Group, msg.Name, msg.FetchedSpecs, msg.RemainingRepos, msg.AllowEdits, msg.LoadClaudeMD))
	} else {
		// All fetched — advance to "Creating worktrees"
		if a.creationOverlay != nil {
			a.creationOverlay.AdvanceStep()
		}
		cmds = append(cmds, a.createGroupWorkspaceFromSpecs(msg.Group, msg.Name, msg.FetchedSpecs, msg.AllowEdits, msg.LoadClaudeMD))
	}
	return cmds
}

// createGroupWorkspaceFromSpecs creates worktrees from pre-built specs (step 2 of group creation).
func (a *App) createGroupWorkspaceFromSpecs(group *data.ProjectGroup, name string, specs []git.RepoSpec, allowEdits, loadClaudeMD bool) tea.Cmd {
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				logging.Error("panic in createGroupWorkspaceFromSpecs: %v", r)
				msg = messages.GroupWorkspaceCreateFailed{
					Err: fmt.Errorf("create group workspace panicked: %v", r),
				}
			}
		}()

		// Create all worktrees
		if err := git.CreateGroupWorkspace(specs); err != nil {
			return messages.GroupWorkspaceCreateFailed{Err: err}
		}

		// Build group workspace — Primary.Root is the group workspace directory
		// (parent of all repo worktrees), all repos go into Secondary.
		groupRoot := filepath.Join(
			a.config.Paths.GroupsWorkspacesRoot,
			group.Name,
			name,
		)
		primary := data.NewWorkspace(name, name, specs[0].Base, group.Repos[0].Path, groupRoot)
		primary.AllowEdits = allowEdits

		var secondary []data.Workspace
		for i := 0; i < len(specs); i++ {
			ws := data.NewWorkspace(name, name, specs[i].Base, group.Repos[i].Path, specs[i].WorkspacePath)
			ws.AllowEdits = allowEdits
			secondary = append(secondary, *ws)
		}

		gw := &data.GroupWorkspace{
			Name:         name,
			Created:      time.Now(),
			GroupName:    group.Name,
			Primary:      *primary,
			Secondary:    secondary,
			AllowEdits:   allowEdits,
			LoadClaudeMD: loadClaudeMD,
			Assistant:    "claude",
			Profile:      group.Profile,
			ScriptMode:   "nonconcurrent",
			Env:          make(map[string]string),
		}

		// Save metadata
		if err := a.workspaces.SaveGroupWorkspace(gw); err != nil {
			// Rollback worktrees
			git.RemoveGroupWorkspace(specs)
			return messages.GroupWorkspaceCreateFailed{Workspace: gw, Err: err}
		}

		return messages.GroupWorkspaceCreated{Workspace: gw}
	}
}

// deleteGroupWorkspace deletes a group workspace's worktrees and metadata.
func (a *App) deleteGroupWorkspace(group *data.ProjectGroup, gw *data.GroupWorkspace) tea.Cmd {
	if group == nil || gw == nil {
		return func() tea.Msg {
			return messages.GroupWorkspaceDeleteFailed{
				Group:     group,
				Workspace: gw,
				Err:       fmt.Errorf("missing group or workspace"),
			}
		}
	}

	// Clear UI if deleting the active group workspace
	if a.activeGroupWs != nil && a.activeGroupWs.ID() == gw.ID() {
		a.goHome()
	}

	return func() tea.Msg {
		deleteGroupWorkspaceSync(a, group, gw)
		return messages.GroupWorkspaceDeleted{
			Group:     group,
			Workspace: gw,
		}
	}
}

// deleteGroupWorkspaceSync performs the actual deletion synchronously.
func deleteGroupWorkspaceSync(a *App, group *data.ProjectGroup, gw *data.GroupWorkspace) {
	specs := buildSpecsFromGroupWorkspace(group, gw)
	git.RemoveGroupWorkspace(specs)
	// Clean up the group workspace directory (Primary.Root) and any leftover
	// untracked files (e.g. .claude/settings.local.json).
	_ = os.RemoveAll(gw.Primary.Root)
	_ = a.workspaces.DeleteGroupWorkspace(gw.ID())
}

func buildSpecsFromGroupWorkspace(group *data.ProjectGroup, gw *data.GroupWorkspace) []git.RepoSpec {
	var specs []git.RepoSpec
	for _, ws := range gw.Secondary {
		specs = append(specs, git.RepoSpec{
			RepoPath:      ws.Repo,
			RepoName:      filepath.Base(ws.Repo),
			WorkspacePath: ws.Root,
			Branch:        gw.Name,
		})
	}
	return specs
}

// generateGroupWorkspaceName generates a unique name for a group workspace.
func generateGroupWorkspaceName(group *data.ProjectGroup) string {
	const maxAttempts = 50
	for range maxAttempts {
		name := fmt.Sprintf("%s-%s-%s",
			group.Name,
			randomAnimals[rand.IntN(len(randomAnimals))],
			randomColors[rand.IntN(len(randomColors))],
		)
		if group.FindWorkspaceByName(name) == nil {
			return name
		}
	}
	return fmt.Sprintf("%s-%s-%s-%d",
		group.Name,
		randomAnimals[rand.IntN(len(randomAnimals))],
		randomColors[rand.IntN(len(randomColors))],
		rand.IntN(1000),
	)
}
