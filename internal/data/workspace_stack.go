package data

import (
	"sort"
	"strings"
)

// WorkspaceStackEntry is a flattened workspace tree entry.
type WorkspaceStackEntry struct {
	Workspace *Workspace
	Depth     int
}

// WorkspaceLessFunc orders workspaces for stack roots and siblings.
type WorkspaceLessFunc func(left, right *Workspace) bool

// ComposeChildWorkspaceName prefixes a child workspace with its parent name
// unless the child already carries the same prefix.
func ComposeChildWorkspaceName(parentName, childName string) string {
	parent := strings.TrimSpace(parentName)
	child := strings.TrimSpace(childName)
	if parent == "" {
		return child
	}
	if child == "" {
		return parent
	}
	if child == parent ||
		strings.HasPrefix(child, parent+".") ||
		strings.HasPrefix(child, parent+"-") ||
		strings.HasPrefix(child, parent+"_") {
		return child
	}
	return parent + "." + child
}

// ApplyStackParent copies parent stack metadata onto a child workspace.
func ApplyStackParent(child, parent *Workspace, parentBranch string) {
	if child == nil || parent == nil {
		return
	}
	child.ParentWorkspaceID = parent.ID()
	child.ParentBranch = strings.TrimSpace(parentBranch)
	child.StackRootWorkspaceID = parent.EffectiveStackRootWorkspaceID()
	child.StackDepth = parent.StackDepth + 1
	if child.StackDepth < 1 {
		child.StackDepth = 1
	}
	if child.Base == "" {
		child.Base = child.ParentBranch
	}
}

// WorkspaceCreatedDescLess orders workspaces by newest-first creation time,
// then name, then root for stable sibling ordering.
func WorkspaceCreatedDescLess(left, right *Workspace) bool {
	if left == nil || right == nil {
		return left != nil
	}
	if left.Created.Equal(right.Created) {
		if left.Name == right.Name {
			return left.Root < right.Root
		}
		return left.Name < right.Name
	}
	return left.Created.After(right.Created)
}

// FlattenWorkspaceTree returns a depth-first, stack-aware ordering of the
// provided workspaces. Missing parents are treated as detached roots.
func FlattenWorkspaceTree(workspaces []*Workspace, less WorkspaceLessFunc) []WorkspaceStackEntry {
	if len(workspaces) == 0 {
		return nil
	}

	nodes := make(map[string]*Workspace, len(workspaces))
	ordered := make([]*Workspace, 0, len(workspaces))
	for _, ws := range workspaces {
		if ws == nil {
			continue
		}
		id := string(ws.ID())
		if _, exists := nodes[id]; exists {
			continue
		}
		nodes[id] = ws
		ordered = append(ordered, ws)
	}

	children := make(map[string][]*Workspace, len(ordered))
	roots := make([]*Workspace, 0, len(ordered))
	for _, ws := range ordered {
		parentID := strings.TrimSpace(string(ws.ParentWorkspaceID))
		if parentID == "" {
			roots = append(roots, ws)
			continue
		}
		if _, ok := nodes[parentID]; !ok {
			roots = append(roots, ws)
			continue
		}
		children[parentID] = append(children[parentID], ws)
	}

	sortWorkspaces := func(items []*Workspace) {
		sort.SliceStable(items, func(i, j int) bool {
			if less == nil {
				return WorkspaceCreatedDescLess(items[i], items[j])
			}
			if less(items[i], items[j]) {
				return true
			}
			if less(items[j], items[i]) {
				return false
			}
			return string(items[i].ID()) < string(items[j].ID())
		})
	}

	sortWorkspaces(roots)

	entries := make([]WorkspaceStackEntry, 0, len(ordered))
	visited := make(map[string]bool, len(ordered))
	var appendTree func(ws *Workspace, depth int)
	appendTree = func(ws *Workspace, depth int) {
		if ws == nil {
			return
		}
		id := string(ws.ID())
		if visited[id] {
			return
		}
		visited[id] = true
		entries = append(entries, WorkspaceStackEntry{Workspace: ws, Depth: depth})
		kids := children[id]
		sortWorkspaces(kids)
		for _, child := range kids {
			appendTree(child, depth+1)
		}
	}

	for _, root := range roots {
		appendTree(root, 0)
	}

	if len(visited) == len(ordered) {
		return entries
	}

	remaining := make([]*Workspace, 0, len(ordered)-len(visited))
	for _, ws := range ordered {
		if !visited[string(ws.ID())] {
			remaining = append(remaining, ws)
		}
	}
	sortWorkspaces(remaining)
	for _, ws := range remaining {
		appendTree(ws, 0)
	}

	return entries
}
