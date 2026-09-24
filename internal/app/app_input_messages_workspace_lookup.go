package app

import (
	"strings"

	"github.com/andyrewlee/amux/internal/data"
)

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
