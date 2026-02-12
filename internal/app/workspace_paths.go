package app

import (
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
)

var discoverWorkspacesFn = git.DiscoverWorkspaces

// projectNameSegment extracts a filesystem-safe name from a project.
// Returns ("", false) for nil project, empty name, ".", "..", or names with "/" or "\".
func projectNameSegment(project *data.Project) (string, bool) {
	if project == nil {
		return "", false
	}
	name := strings.TrimSpace(project.Name)
	if name == "" {
		name = filepath.Base(strings.TrimSpace(project.Path))
	}
	if name == "" || name == "." || name == ".." {
		return "", false
	}
	if strings.ContainsAny(name, "/\\") {
		return "", false
	}
	return name, true
}

// primaryManagedProjectRoot returns filepath.Join(workspacesRoot, projectNameSegment).
// Returns "" if workspacesRoot is empty or projectNameSegment fails.
func primaryManagedProjectRoot(workspacesRoot string, project *data.Project) string {
	root := strings.TrimSpace(workspacesRoot)
	if root == "" {
		return ""
	}
	seg, ok := projectNameSegment(project)
	if !ok {
		return ""
	}
	return filepath.Join(root, seg)
}

// managedProjectRoots returns alias-expanded roots via workspacePathAliases.
func managedProjectRoots(workspacesRoot string, project *data.Project) []string {
	primary := primaryManagedProjectRoot(workspacesRoot, project)
	if primary == "" {
		return nil
	}
	return workspacePathAliases(primary)
}

// isManagedWorkspacePath returns true if workspacesRoot is empty (legacy/test)
// OR path is within workspacesRoot.
func isManagedWorkspacePath(workspacesRoot, path string) bool {
	root := strings.TrimSpace(workspacesRoot)
	if root == "" {
		return true
	}
	if strings.TrimSpace(path) == "" {
		return false
	}
	return pathWithinAliases(workspacePathAliases(root), workspacePathAliases(path))
}

// isManagedWorkspacePathForProject returns true if workspacesRoot is empty (legacy)
// OR path is within managedProjectRoots.
func isManagedWorkspacePathForProject(workspacesRoot string, project *data.Project, path string) bool {
	root := strings.TrimSpace(workspacesRoot)
	if root == "" {
		return true
	}
	roots := managedProjectRoots(workspacesRoot, project)
	if len(roots) == 0 {
		return false
	}
	if strings.TrimSpace(path) == "" {
		return false
	}
	return pathWithinAliases(roots, workspacePathAliases(path))
}

// isPathWithin returns true if candidate is strictly nested under root (excludes same-path).
// NOTE: differs from existing pathWithin which includes rel=="." as true.
func isPathWithin(root, candidate string) bool {
	if root == "" || candidate == "" {
		return false
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	// Exclude same path (rel == ".") — must be strictly nested.
	if rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// isLegacyManagedWorkspaceDeletePath returns true only if:
// 1. ws.Root is within the broad workspacesRoot
// 2. ws.Root is discoverable via git worktree list for this project
func isLegacyManagedWorkspaceDeletePath(workspacesRoot string, project *data.Project, ws *data.Workspace) bool {
	if project == nil || ws == nil {
		return false
	}
	if !isManagedWorkspacePath(workspacesRoot, ws.Root) {
		return false
	}
	discovered, err := discoverWorkspacesFn(project)
	if err != nil {
		return false
	}
	wsRoot := data.NormalizePath(ws.Root)
	for _, d := range discovered {
		if data.NormalizePath(d.Root) == wsRoot {
			return true
		}
	}
	return false
}
