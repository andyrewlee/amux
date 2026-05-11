package git

import (
	"path/filepath"
	"strings"
)

func isLegacyManagedWorkspacePathForRepo(repoPath, workspacePath string) bool {
	if strings.TrimSpace(repoPath) == "" || strings.TrimSpace(workspacePath) == "" {
		return false
	}

	repoSegments := managedWorkspaceRepoSegments(repoPath)
	if len(repoSegments) == 0 {
		return false
	}

	workspaceRoots := managedWorkspacesRootAliases()
	for _, workspaceRoot := range workspaceRoots {
		for repoSegment := range repoSegments {
			repoRoot := filepath.Join(workspaceRoot, repoSegment)
			for _, alias := range comparablePaths(workspacePath) {
				if _, ok := pathWithinManagedRoot(repoRoot, alias); ok {
					return true
				}
			}
		}
	}
	if len(workspaceRoots) > 0 {
		return false
	}
	for _, alias := range comparablePaths(workspacePath) {
		if hasManagedWorkspaceAncestorForRepo(alias, repoSegments) {
			return true
		}
	}
	return false
}

func managedWorkspaceRepoSegments(repoPath string) map[string]struct{} {
	segments := make(map[string]struct{}, 4)
	for _, alias := range comparablePaths(repoPath) {
		segment := filepath.Base(alias)
		if isSafeManagedWorkspaceRepoSegment(segment) {
			segments[segment] = struct{}{}
		}
	}
	return segments
}

func isSafeManagedWorkspaceRepoSegment(segment string) bool {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return false
	}
	segment = filepath.Clean(segment)
	if segment == "" || segment == "." || segment == ".." {
		return false
	}
	return !strings.ContainsAny(segment, `/\`)
}

func hasManagedWorkspaceAncestorForRepo(path string, repoSegments map[string]struct{}) bool {
	cleanPath := filepath.Clean(path)
	root := filepath.VolumeName(cleanPath) + string(filepath.Separator)
	for current := cleanPath; current != root && current != "." && current != ""; current = filepath.Dir(current) {
		if filepath.Base(current) == "workspaces" && filepath.Base(filepath.Dir(current)) == ".amux" {
			rel, err := filepath.Rel(current, cleanPath)
			if err != nil {
				return false
			}
			parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
			if len(parts) < 2 {
				return false
			}
			_, ok := repoSegments[parts[0]]
			return ok
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return false
}
