package sidebar

import (
	"path/filepath"
	"strings"
)

func (m *ProjectTree) rebaseTreePaths(oldRoot, newRoot string) bool {
	if m.root == nil {
		return false
	}
	oldClean := filepath.Clean(strings.TrimSpace(oldRoot))
	newClean := filepath.Clean(strings.TrimSpace(newRoot))
	if oldClean == "" || newClean == "" {
		return false
	}

	var walk func(*projectTreeNode) bool
	walk = func(node *projectTreeNode) bool {
		if node == nil {
			return true
		}
		rebased, ok := rebasePathFromRoot(node.Path, oldClean, newClean)
		if !ok {
			return false
		}
		node.Path = rebased
		for _, child := range node.Children {
			if !walk(child) {
				return false
			}
		}
		return true
	}

	if !walk(m.root) {
		return false
	}
	m.root.Name = filepath.Base(newClean)
	return true
}

func rebasePathFromRoot(path, oldRoot, newRoot string) (string, bool) {
	candidate := filepath.Clean(strings.TrimSpace(path))
	if candidate == "" {
		return "", false
	}

	if rebased, ok := rebasePathWithBase(candidate, oldRoot, newRoot); ok {
		return rebased, true
	}

	oldCanonical := canonicalWorkspacePath(oldRoot)
	newCanonical := canonicalWorkspacePath(newRoot)
	pathCanonical := canonicalWorkspacePath(candidate)
	if oldCanonical == "" || newCanonical == "" || pathCanonical == "" {
		return "", false
	}
	return rebasePathWithBase(pathCanonical, oldCanonical, newCanonical)
}

func rebasePathWithBase(path, oldBase, newBase string) (string, bool) {
	rel, err := filepath.Rel(oldBase, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	if rel == "." {
		return newBase, true
	}
	return filepath.Join(newBase, rel), true
}
