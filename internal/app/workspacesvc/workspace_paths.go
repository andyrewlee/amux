package workspacesvc

import (
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
)

// projectNameSegment extracts a filesystem-safe name from a project.
// Returns ("", false) for nil project, empty name, ".", "..", or names with "/" or "\".
func projectNameSegment(project *data.Project) (string, bool) {
	if project == nil {
		return "", false
	}
	name := strings.TrimSpace(project.Name)
	if strings.ContainsAny(name, "/\\") {
		return "", false
	}
	if name == "" {
		name = filepath.Base(strings.TrimSpace(project.Path))
	}
	name = filepath.Clean(name)
	if name == "" || name == "." || name == ".." {
		return "", false
	}
	// Re-check separators after fallback/clean to reject values like "/".
	if strings.ContainsAny(name, "/\\") {
		return "", false
	}
	return name, true
}

// managedProjectRoots returns alias-expanded roots via workspacePathAliases.
//
// Security note: this intentionally widens accepted managed roots to include the
// project path basename aliases in addition to project.Name. Destructive flows
// must pair this with a repo/path identity check (for example, DeleteWorkspace
// validates ws.Repo matches project.Path) to avoid cross-project collisions.
func managedProjectRoots(workspacesRoot string, project *data.Project) []string {
	root := strings.TrimSpace(workspacesRoot)
	if root == "" || project == nil {
		return nil
	}

	segments := make(map[string]struct{}, 4)
	if seg, ok := projectNameSegment(project); ok {
		segments[seg] = struct{}{}
	}
	// Also trust the project path basename(s). This handles cases where
	// project.Name drifts from the canonical repo basename (e.g. symlink aliases)
	// while keeping checks confined under workspacesRoot.
	for _, alias := range workspacePathAliases(project.Path) {
		seg := filepath.Base(alias)
		if isSafeProjectPathSegment(seg) {
			segments[seg] = struct{}{}
		}
	}
	if len(segments) == 0 {
		return nil
	}

	roots := make(map[string]struct{}, len(segments)*2)
	for seg := range segments {
		candidate := filepath.Join(root, seg)
		for _, alias := range workspacePathAliases(candidate) {
			roots[alias] = struct{}{}
		}
	}

	result := make([]string, 0, len(roots))
	for value := range roots {
		result = append(result, value)
	}
	return result
}

func isSafeProjectPathSegment(segment string) bool {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return false
	}
	segment = filepath.Clean(segment)
	if segment == "" || segment == "." || segment == ".." {
		return false
	}
	return !strings.ContainsAny(segment, "/\\")
}

// isManagedWorkspaceChildPathForProject returns true only when path is an
// immediate child of a managed project root. Destructive flows MUST use this:
// amux creates workspaces only at filepath.Join(projectRoot, name), so the
// project root itself and any deeper descendant are both untrusted — a forged
// `git worktree list` entry naming either must never reach a
// delete/shelve/staging path. Fails closed on empty workspacesRoot.
func isManagedWorkspaceChildPathForProject(workspacesRoot string, project *data.Project, path string) bool {
	root := strings.TrimSpace(workspacesRoot)
	if root == "" {
		return false
	}
	roots := managedProjectRoots(workspacesRoot, project)
	if len(roots) == 0 {
		return false
	}
	if strings.TrimSpace(path) == "" {
		return false
	}
	return pathWithinAliasesImmediateChild(roots, workspacePathAliases(path))
}
