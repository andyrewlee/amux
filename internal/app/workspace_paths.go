package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
)

func (s *workspaceService) isManagedWorkspacePath(path string) bool {
	root := data.NormalizePath(strings.TrimSpace(s.workspacesRoot))
	if root == "" {
		// Tests and legacy call sites may construct a service without a managed root.
		return true
	}
	candidate := data.NormalizePath(strings.TrimSpace(path))
	if candidate == "" {
		return false
	}
	return isPathWithin(root, candidate)
}

func (s *workspaceService) isManagedWorkspacePathForProject(project *data.Project, path string) bool {
	if !s.isManagedWorkspacePath(path) {
		return false
	}
	root := data.NormalizePath(strings.TrimSpace(s.workspacesRoot))
	if root == "" {
		return true
	}
	candidate := data.NormalizePath(strings.TrimSpace(path))
	if candidate == "" {
		return false
	}
	roots := s.managedProjectRoots(project)
	for _, projectRoot := range roots {
		if isPathWithin(projectRoot, candidate) {
			return true
		}
	}
	return false
}

func (s *workspaceService) primaryManagedProjectRoot(project *data.Project) string {
	root := data.NormalizePath(strings.TrimSpace(s.workspacesRoot))
	if root == "" {
		return ""
	}
	projectName, ok := projectNameSegment(project)
	if !ok {
		return ""
	}
	return data.NormalizePath(filepath.Join(root, projectName))
}

func (s *workspaceService) pendingWorkspace(project *data.Project, name, base string) (*data.Workspace, bool) {
	if project == nil {
		return nil, false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, false
	}
	base = strings.TrimSpace(base)
	if base == "" {
		base = "HEAD"
	}
	projectRoot, validScope := s.pendingProjectRoot(project)
	workspacePath := filepath.Join(projectRoot, name)
	return data.NewWorkspace(name, name, base, project.Path, workspacePath), validScope
}

func (s *workspaceService) pendingProjectRoot(project *data.Project) (string, bool) {
	managedProjectRoot := s.primaryManagedProjectRoot(project)
	if managedProjectRoot == "" {
		fallbackRoot := filepath.Join(s.workspacesRoot, project.Name)
		if data.NormalizePath(strings.TrimSpace(s.workspacesRoot)) != "" {
			return fallbackRoot, false
		}
		return fallbackRoot, true
	}
	return managedProjectRoot, true
}

func (s *workspaceService) managedProjectRoots(project *data.Project) []string {
	primary := s.primaryManagedProjectRoot(project)
	if primary == "" {
		return nil
	}
	return []string{primary}
}

func (s *workspaceService) isLegacyManagedWorkspaceDeletePath(project *data.Project, ws *data.Workspace) bool {
	if project == nil || ws == nil {
		return false
	}
	if !s.isManagedWorkspacePath(ws.Root) {
		return false
	}
	// Only allow the legacy delete compatibility path for existing directories.
	// If the path is missing we cannot verify ownership and should not relax scope checks.
	info, err := os.Stat(ws.Root)
	if err != nil || !info.IsDir() {
		return false
	}
	if discoverWorkspacesFn == nil {
		return false
	}
	discovered, err := discoverWorkspacesFn(project)
	if err != nil {
		return false
	}
	target := data.NormalizePath(ws.Root)
	for _, candidate := range discovered {
		if data.NormalizePath(candidate.Root) == target {
			return true
		}
	}
	return false
}

func projectNameSegment(project *data.Project) (string, bool) {
	if project == nil {
		return "", false
	}
	name := strings.TrimSpace(project.Name)
	if name == "" {
		name = filepath.Base(strings.TrimSpace(project.Path))
	}
	if name == "" {
		return "", false
	}
	name = filepath.Clean(name)
	if name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", false
	}
	return name, true
}

func isPathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	if rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
