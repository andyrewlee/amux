package app

import (
	"strings"

	"github.com/andyrewlee/amux/internal/data"
)

// rootsReferToSameWorkspace answers the PATH question "does this
// message-stamped path address the workspace at this other path?" — used to
// route git-status/file-watcher results (which carry only a root path) to
// the active workspace. Root-only compare is deliberate here: two live
// workspaces cannot share a canonical root (distinct worktrees), and a
// status produced for a path belongs to whatever workspace occupies that
// path now.
//
// Deliberately different from sidebar's sameWorkspaceByCanonicalPaths, which
// answers the IDENTITY question "is this the same workspace record?" and
// adds a repo comparison to distinguish a workspace re-created at the same
// path under a different repo — a discrimination a bare path message cannot
// make and does not need.
func rootsReferToSameWorkspace(left, right string) bool {
	leftTrimmed := strings.TrimSpace(left)
	rightTrimmed := strings.TrimSpace(right)
	if leftTrimmed == "" || rightTrimmed == "" {
		return false
	}
	if leftTrimmed == rightTrimmed {
		return true
	}
	return canonicalPathForMatch(leftTrimmed) == canonicalPathForMatch(rightTrimmed)
}

func (a *App) findWorkspaceAndProjectByID(id string) (*data.Workspace, *data.Project) {
	if id == "" {
		return nil, nil
	}
	var foundWs *data.Workspace
	var foundProject *data.Project
	a.eachWorkspaceUntil(func(ws *data.Workspace, project *data.Project) bool {
		if string(ws.ID()) == id {
			foundWs, foundProject = ws, project
			return true
		}
		return false
	})
	return foundWs, foundProject
}

func (a *App) findWorkspaceAndProjectByCanonicalPaths(repoPath, rootPath string) (*data.Workspace, *data.Project) {
	targetRepo := canonicalPathForMatch(repoPath)
	targetRoot := canonicalPathForMatch(rootPath)
	if targetRepo == "" && targetRoot == "" {
		return nil, nil
	}
	var foundWs *data.Workspace
	var foundProject *data.Project
	a.eachWorkspaceUntil(func(ws *data.Workspace, project *data.Project) bool {
		repoCanonical := canonicalPathForMatch(ws.Repo)
		rootCanonical := canonicalPathForMatch(ws.Root)
		if targetRoot != "" && rootCanonical != targetRoot {
			return false
		}
		if targetRepo != "" && repoCanonical != targetRepo {
			return false
		}
		if targetRoot == "" && targetRepo != "" && repoCanonical != targetRepo {
			return false
		}
		foundWs, foundProject = ws, project
		return true
	})
	return foundWs, foundProject
}

func (a *App) findProjectByPath(path string) *data.Project {
	if path == "" {
		return nil
	}
	targetCanonical := canonicalProjectPathForMatch(path)
	for i := range a.projects {
		project := &a.projects[i]
		if project.Path == path {
			return project
		}
		if targetCanonical == "" {
			continue
		}
		if canonicalProjectPathForMatch(project.Path) == targetCanonical {
			return project
		}
	}
	return nil
}

func canonicalProjectPathForMatch(path string) string {
	return canonicalPathForMatch(path)
}

func canonicalPathForMatch(path string) string {
	return data.CanonicalPath(path)
}
