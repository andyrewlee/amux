package service

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/andyrewlee/medusa/internal/config"
	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/git"
	"github.com/andyrewlee/medusa/internal/logging"
)

// GroupService manages multi-repo project groups and their workspaces.
type GroupService struct {
	registry   *data.Registry
	workspaces *data.WorkspaceStore
	config     *config.Config
	eventBus   *EventBus
}

// NewGroupService creates a group service.
func NewGroupService(registry *data.Registry, workspaces *data.WorkspaceStore, cfg *config.Config, bus *EventBus) *GroupService {
	return &GroupService{
		registry:   registry,
		workspaces: workspaces,
		config:     cfg,
		eventBus:   bus,
	}
}

// ListGroups returns all project groups with their workspaces.
func (s *GroupService) ListGroups() ([]data.ProjectGroup, error) {
	groups, err := s.registry.LoadGroups()
	if err != nil {
		return nil, fmt.Errorf("loading groups: %w", err)
	}

	for i := range groups {
		gwPtrs, err := s.workspaces.ListGroupWorkspacesByGroup(groups[i].Name)
		if err != nil {
			logging.Warn("Failed to load group workspaces for %s: %v", groups[i].Name, err)
			continue
		}
		gwList := make([]data.GroupWorkspace, len(gwPtrs))
		for j, gw := range gwPtrs {
			gwList[j] = *gw
		}
		groups[i].Workspaces = gwList
	}

	return groups, nil
}

// CreateGroup creates a new project group.
func (s *GroupService) CreateGroup(name string, repoPaths []string, profile string) error {
	if name == "" {
		return fmt.Errorf("group name is required")
	}
	if len(repoPaths) == 0 {
		return fmt.Errorf("at least one repository path is required")
	}

	// Validate all repos
	var repos []data.GroupRepo
	for _, path := range repoPaths {
		if !git.IsGitRepository(path) {
			return fmt.Errorf("not a git repository: %s", path)
		}
		repos = append(repos, data.GroupRepo{
			Path: path,
			Name: filepath.Base(path),
		})
	}

	if err := s.registry.AddGroup(name, repos, profile); err != nil {
		return fmt.Errorf("saving group: %w", err)
	}

	s.eventBus.Publish(NewEvent(EventGroupCreated, map[string]string{"name": name}))
	return nil
}

// DeleteGroup removes a project group and optionally its workspaces.
func (s *GroupService) DeleteGroup(name string) error {
	// Delete all group workspaces first
	gwList, err := s.workspaces.ListGroupWorkspacesByGroup(name)
	if err != nil {
		logging.Warn("Failed to list group workspaces for deletion: %v", err)
	}
	for _, gw := range gwList {
		if err := s.deleteGroupWorkspaceInternal(gw); err != nil {
			logging.Warn("Failed to delete group workspace %s: %v", gw.Name, err)
		}
	}

	if err := s.registry.RemoveGroup(name); err != nil {
		return fmt.Errorf("deleting group: %w", err)
	}

	s.eventBus.Publish(NewEvent(EventGroupDeleted, map[string]string{"name": name}))
	return nil
}

// RenameGroup renames a project group.
func (s *GroupService) RenameGroup(oldName, newName string) error {
	if oldName == "" || newName == "" {
		return fmt.Errorf("both old and new names are required")
	}

	if err := s.registry.RenameGroup(oldName, newName); err != nil {
		return fmt.Errorf("renaming group: %w", err)
	}

	return nil
}

// CreateGroupWorkspace creates a new workspace spanning all repos in a group.
func (s *GroupService) CreateGroupWorkspace(opts GroupWorkspaceOpts) (*data.GroupWorkspace, error) {
	if opts.GroupName == "" || opts.Name == "" {
		return nil, fmt.Errorf("group name and workspace name are required")
	}

	groups, err := s.registry.LoadGroups()
	if err != nil {
		return nil, fmt.Errorf("loading groups: %w", err)
	}

	var group *data.ProjectGroup
	for i := range groups {
		if groups[i].Name == opts.GroupName {
			group = &groups[i]
			break
		}
	}
	if group == nil {
		return nil, fmt.Errorf("group '%s' not found", opts.GroupName)
	}

	if len(group.Repos) == 0 {
		return nil, fmt.Errorf("group '%s' has no repositories", opts.GroupName)
	}

	// Create group workspace root directory
	groupRoot := filepath.Join(s.config.Paths.WorkspacesRoot, "groups", opts.GroupName, opts.Name)
	if err := os.MkdirAll(groupRoot, 0755); err != nil {
		return nil, fmt.Errorf("creating group workspace root: %w", err)
	}

	// Create worktrees for each repo in the group
	var secondary []data.Workspace
	for _, repo := range group.Repos {
		base, err := git.GetFreshRemoteBase(repo.Path)
		if err != nil {
			base = "HEAD"
		}

		branchName := fmt.Sprintf("%s-%s", opts.GroupName, opts.Name)
		worktreePath := filepath.Join(groupRoot, repo.Name)

		if err := git.CreateWorkspace(repo.Path, worktreePath, branchName, base); err != nil {
			// Cleanup on failure
			_ = os.RemoveAll(groupRoot)
			return nil, fmt.Errorf("creating worktree for %s: %w", repo.Name, err)
		}

		ws := data.Workspace{
			Name:   repo.Name,
			Branch: branchName,
			Base:   base,
			Repo:   repo.Path,
			Root:   worktreePath,
		}
		secondary = append(secondary, ws)
	}

	// Build group workspace
	primary := data.Workspace{
		Name: opts.Name,
		Root: groupRoot,
		Repo: group.Repos[0].Path,
	}

	gw := &data.GroupWorkspace{
		Name:            opts.Name,
		Created:         time.Now(),
		GroupName:       opts.GroupName,
		Primary:         primary,
		Secondary:       secondary,
		AllowEdits:      opts.AllowEdits,
		Isolated:        opts.Isolated,
		SkipPermissions: opts.SkipPermissions,
		Assistant:       "claude",
		Profile:         group.Profile,
	}

	if err := s.workspaces.SaveGroupWorkspace(gw); err != nil {
		_ = os.RemoveAll(groupRoot)
		return nil, fmt.Errorf("saving group workspace: %w", err)
	}

	s.eventBus.Publish(NewEvent(EventGroupWorkspaceCreated, map[string]string{
		"group": opts.GroupName,
		"name":  opts.Name,
	}))
	return gw, nil
}

// DeleteGroupWorkspace removes a group workspace and its worktrees.
func (s *GroupService) DeleteGroupWorkspace(groupName, wsID string) error {
	gwList, err := s.workspaces.ListGroupWorkspacesByGroup(groupName)
	if err != nil {
		return fmt.Errorf("listing group workspaces: %w", err)
	}

	for _, gw := range gwList {
		if string(gw.ID()) == wsID {
			return s.deleteGroupWorkspaceInternal(gw)
		}
	}

	return fmt.Errorf("group workspace %s not found in group %s", wsID, groupName)
}

func (s *GroupService) deleteGroupWorkspaceInternal(gw *data.GroupWorkspace) error {
	// Remove all worktrees
	for _, ws := range gw.Secondary {
		if err := git.RemoveWorkspace(ws.Repo, ws.Root); err != nil {
			logging.Warn("Failed to remove worktree %s: %v", ws.Root, err)
		}
		if err := git.DeleteBranch(ws.Repo, ws.Branch); err != nil {
			logging.Warn("Failed to delete branch %s: %v", ws.Branch, err)
		}
	}

	// Remove group workspace root
	_ = os.RemoveAll(gw.Primary.Root)

	// Delete metadata
	_ = s.workspaces.DeleteGroupWorkspace(gw.ID())

	s.eventBus.Publish(NewEvent(EventGroupWorkspaceDeleted, map[string]string{
		"group": gw.GroupName,
		"name":  gw.Name,
	}))
	return nil
}
