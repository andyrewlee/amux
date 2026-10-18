package sidebar

import "strings"

// Filter/predicate support for the project tree — the flat-list filter
// predicate, the filter status row's height, and the FilterActive accessor
// the tab bar and app paste router gate on. The FilterInput widget itself
// lives on the model in project_tree.go.

// treeNodeVisible reports whether a node belongs in the filtered list: its
// own path matches the query (case-insensitive substring, the Changes
// shape) or a loaded descendant's does. An unexpanded directory's unloaded
// children cannot match — the honest bound of lazy loading. Nodes carry
// their lowercased path so a query edit never re-lowercases the tree.
func treeNodeVisible(node *projectTreeNode, query string) bool {
	if strings.Contains(node.lowerPath, query) {
		return true
	}
	for _, child := range node.Children {
		if treeNodeVisible(child, query) {
			return true
		}
	}
	return false
}

// filterLineCount reports the height of the filter status row rendered
// above the node list — independent of showKeymapHints since the row is
// functional state.
func (m *ProjectTree) filterLineCount() int {
	if m.filter.Active() || m.filter.Query() != "" {
		return 1
	}
	return 0
}

// FilterActive reports whether the tree is in filter-input mode — the
// tab bar must not steal digit keys meant for the filter.
func (m *ProjectTree) FilterActive() bool {
	return m.filter.Active()
}
