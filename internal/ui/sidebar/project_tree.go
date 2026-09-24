package sidebar

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// projectTreeNode represents a file or directory in the tree
type projectTreeNode struct {
	Name     string
	Path     string
	IsDir    bool
	Expanded bool
	Depth    int
	Children []*projectTreeNode
	Parent   *projectTreeNode
}

// ProjectTree is a nerdtree-like file browser
type ProjectTree struct {
	workspace    *data.Workspace
	root         *projectTreeNode
	flatNodes    []*projectTreeNode // flattened visible nodes for rendering
	cursor       int
	scrollOffset int
	focused      bool

	width           int
	height          int
	showKeymapHints bool
	showHidden      bool

	styles common.Styles

	// contentVersion is a monotonic version of every input that shapes View
	// output. INVARIANT: every update path that changes what View renders
	// MUST call markContentDirty (Update marks at the funnel), or the
	// compose-time gate in internal/app will keep reusing a stale drawable.
	contentVersion uint64
	// contentBuilds counts View invocations; test instrumentation for the
	// compose-time skip gate in internal/app.
	contentBuilds uint64
}

// markContentDirty bumps contentVersion; see the field's invariant.
func (m *ProjectTree) markContentDirty() { m.contentVersion++ }

// ContentVersion returns the monotonic version of the inputs to View. The
// compose layer in internal/app skips rebuilding the content string while
// this version and the compose geometry are unchanged.
func (m *ProjectTree) ContentVersion() uint64 { return m.contentVersion }

// ContentBuildCount reports how many times View has been invoked. Test
// instrumentation for the compose-time skip gate; not for production use.
func (m *ProjectTree) ContentBuildCount() uint64 { return m.contentBuilds }

// NewProjectTree creates a new project tree model
func NewProjectTree() *ProjectTree {
	return &ProjectTree{
		styles:     common.DefaultStyles(),
		showHidden: true,
	}
}

// SetShowKeymapHints controls whether helper text is rendered.
func (m *ProjectTree) SetShowKeymapHints(show bool) {
	m.showKeymapHints = show
	m.markContentDirty()
}

// SetStyles updates the component's styles (for theme changes).
func (m *ProjectTree) SetStyles(styles common.Styles) {
	m.styles = styles
	m.markContentDirty()
}

// Init initializes the project tree
func (m *ProjectTree) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m *ProjectTree) Update(msg tea.Msg) (*ProjectTree, tea.Cmd) {
	// Handled messages below mutate cursor/scroll/tree state that View
	// renders — marking at the funnel guarantees coverage; a message that
	// early-returns untouched still bumps, which only costs one extra
	// build. (See the contentVersion invariant.)
	defer m.markContentDirty()
	if !m.focused {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		delta := common.ScrollDeltaForHeight(m.visibleHeight(), 10)
		if msg.Button == tea.MouseWheelUp {
			m.moveCursor(-delta)
			return m, nil
		}
		if msg.Button == tea.MouseWheelDown {
			m.moveCursor(delta)
			return m, nil
		}

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			idx, ok := m.rowIndexAt(msg.Y)
			if !ok {
				return m, nil
			}
			m.cursor = idx
			return m, m.handleEnter()
		}

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			m.moveCursor(1)
		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			m.moveCursor(-1)
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter", "o"))):
			return m, m.handleEnter()
		case key.Matches(msg, key.NewBinding(key.WithKeys("l", "right"))):
			// Expand directory
			if m.cursor >= 0 && m.cursor < len(m.flatNodes) {
				node := m.flatNodes[m.cursor]
				if node.IsDir && !node.Expanded {
					m.expandNode(node)
					m.rebuildFlatList()
				}
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("h", "left"))):
			// Collapse directory or go to parent
			if m.cursor >= 0 && m.cursor < len(m.flatNodes) {
				node := m.flatNodes[m.cursor]
				if node.IsDir && node.Expanded {
					node.Expanded = false
					m.rebuildFlatList()
				} else if node.Parent != nil {
					// Find and move to parent
					for i, n := range m.flatNodes {
						if n == node.Parent {
							m.cursor = i
							break
						}
					}
				}
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("."))):
			// Toggle hidden files
			m.showHidden = !m.showHidden
			m.reloadTree()
		case key.Matches(msg, key.NewBinding(key.WithKeys("r"))):
			// Refresh tree
			m.reloadTree()
		}
	}

	return m, nil
}

// handleEnter handles enter/click on a node
func (m *ProjectTree) handleEnter() tea.Cmd {
	if m.cursor < 0 || m.cursor >= len(m.flatNodes) {
		return nil
	}

	node := m.flatNodes[m.cursor]
	if node.IsDir {
		// Toggle expansion
		if node.Expanded {
			node.Expanded = false
		} else {
			m.expandNode(node)
		}
		m.rebuildFlatList()
		return nil
	}

	// File selected - open in vim via center pane
	ws := m.workspace
	path := node.Path
	return func() tea.Msg {
		return messages.OpenFileInVim{
			Path:      path,
			Workspace: ws,
		}
	}
}

// expandNode loads children for a directory node
func (m *ProjectTree) expandNode(node *projectTreeNode) {
	if !node.IsDir || node.Expanded {
		return
	}

	entries, err := os.ReadDir(node.Path)
	if err != nil {
		return
	}

	node.Children = nil
	var dirs, files []*projectTreeNode

	for _, entry := range entries {
		name := entry.Name()
		if !m.showHidden && strings.HasPrefix(name, ".") {
			continue
		}

		child := &projectTreeNode{
			Name:   name,
			Path:   filepath.Join(node.Path, name),
			IsDir:  entry.IsDir(),
			Depth:  node.Depth + 1,
			Parent: node,
		}

		if entry.IsDir() {
			dirs = append(dirs, child)
		} else {
			files = append(files, child)
		}
	}

	// Sort directories and files separately
	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})
	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})

	// Directories first, then files
	node.Children = append(dirs, files...)
	node.Expanded = true
}

// rebuildFlatList flattens the visible tree nodes
func (m *ProjectTree) rebuildFlatList() {
	m.flatNodes = nil
	if m.root == nil {
		return
	}

	var walk func(node *projectTreeNode)
	walk = func(node *projectTreeNode) {
		m.flatNodes = append(m.flatNodes, node)
		if node.IsDir && node.Expanded {
			for _, child := range node.Children {
				walk(child)
			}
		}
	}

	// Start from root's children (don't show root itself)
	if m.root.Expanded {
		for _, child := range m.root.Children {
			walk(child)
		}
	}

	// Clamp cursor
	if m.cursor >= len(m.flatNodes) {
		m.cursor = len(m.flatNodes) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// reloadTree reloads the entire tree from disk, preserving which directories
// were expanded and the cursor position so a refresh (or hidden-toggle) picks up
// new files without collapsing the whole view back to the top level.
func (m *ProjectTree) reloadTree() {
	if m.workspace == nil {
		m.root = nil
		m.flatNodes = nil
		return
	}

	expanded := m.collectExpandedPaths()
	selectedPath := ""
	if m.cursor >= 0 && m.cursor < len(m.flatNodes) {
		selectedPath = m.flatNodes[m.cursor].Path
	}

	m.root = &projectTreeNode{
		Name:   filepath.Base(m.workspace.Root),
		Path:   m.workspace.Root,
		IsDir:  true,
		Depth:  -1, // Root is at depth -1 so children are at 0
		Parent: nil,
	}

	m.expandNode(m.root)
	m.restoreExpansion(m.root, expanded)
	m.rebuildFlatList()

	if selectedPath != "" {
		for i, node := range m.flatNodes {
			if node.Path == selectedPath {
				m.cursor = i
				break
			}
		}
	}
}

// collectExpandedPaths returns the set of directory paths currently expanded.
func (m *ProjectTree) collectExpandedPaths() map[string]bool {
	expanded := map[string]bool{}
	if m.root == nil {
		return expanded
	}
	var collect func(n *projectTreeNode)
	collect = func(n *projectTreeNode) {
		if n.IsDir && n.Expanded {
			expanded[n.Path] = true
		}
		for _, child := range n.Children {
			collect(child)
		}
	}
	collect(m.root)
	return expanded
}

// restoreExpansion re-expands directories (by path) that were expanded before a
// reload and still exist on disk.
func (m *ProjectTree) restoreExpansion(node *projectTreeNode, expanded map[string]bool) {
	for _, child := range node.Children {
		if child.IsDir && expanded[child.Path] {
			m.expandNode(child)
			m.restoreExpansion(child, expanded)
		}
	}
}

func (m *ProjectTree) visibleHeight() int {
	help := m.helpLineCount()
	visible := m.height - help
	if visible < 1 {
		visible = 1
	}
	return visible
}

func (m *ProjectTree) rowIndexAt(screenY int) (int, bool) {
	if len(m.flatNodes) == 0 {
		return -1, false
	}
	help := m.helpLineCount()
	contentHeight := m.height - help
	if screenY < 0 || screenY >= contentHeight {
		return -1, false
	}
	index := m.scrollOffset + screenY
	if index < 0 || index >= len(m.flatNodes) {
		return -1, false
	}
	return index, true
}

func (m *ProjectTree) moveCursor(delta int) {
	if len(m.flatNodes) == 0 {
		return
	}

	newCursor := m.cursor + delta
	if newCursor < 0 {
		newCursor = 0
	}
	if newCursor >= len(m.flatNodes) {
		newCursor = len(m.flatNodes) - 1
	}
	m.cursor = newCursor
}

// SetSize sets the project tree size
func (m *ProjectTree) SetSize(width, height int) {
	if m.width == width && m.height == height {
		return
	}
	m.width = width
	m.height = height
	m.markContentDirty()
}

// Focus sets the focus state
func (m *ProjectTree) Focus() {
	if m.focused {
		return
	}
	m.focused = true
	m.markContentDirty()
}

// Blur removes focus
func (m *ProjectTree) Blur() {
	if !m.focused {
		return
	}
	m.focused = false
	m.markContentDirty()
}

// Focused returns whether the tree is focused
func (m *ProjectTree) Focused() bool {
	return m.focused
}

// SetWorkspace sets the active workspace
func (m *ProjectTree) SetWorkspace(ws *data.Workspace) {
	m.markContentDirty()
	if sameWorkspaceByCanonicalPaths(m.workspace, ws) {
		// Rebind pointer for metadata freshness without resetting navigation state.
		oldRoot := ""
		if m.workspace != nil {
			oldRoot = m.workspace.Root
		}
		m.workspace = ws
		if ws != nil && oldRoot != "" && filepath.Clean(oldRoot) != filepath.Clean(ws.Root) {
			if !m.rebaseTreePaths(oldRoot, ws.Root) {
				// Fallback for mixed-form paths where rebasing isn't computable.
				m.reloadTree()
			}
		}
		return
	}
	m.workspace = ws
	m.cursor = 0
	m.scrollOffset = 0
	m.reloadTree()
}
