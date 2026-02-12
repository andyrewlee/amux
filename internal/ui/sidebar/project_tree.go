package sidebar

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// ProjectTreeNode represents a file or directory in the tree
type ProjectTreeNode struct {
	Name     string
	Path     string
	IsDir    bool
	Expanded bool
	Depth    int
	Children []*ProjectTreeNode
	Parent   *ProjectTreeNode
}

// ProjectTree is a nerdtree-like file browser
type ProjectTree struct {
	workspace    *data.Workspace
	root         *ProjectTreeNode
	flatNodes    []*ProjectTreeNode // flattened visible nodes for rendering
	cursor       int
	scrollOffset int
	focused      bool

	width           int
	height          int
	showKeymapHints bool
	showHidden      bool

	styles common.Styles
}

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
}

// SetStyles updates the component's styles (for theme changes).
func (m *ProjectTree) SetStyles(styles common.Styles) {
	m.styles = styles
}

// Init initializes the project tree
func (m *ProjectTree) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m *ProjectTree) Update(msg tea.Msg) (*ProjectTree, tea.Cmd) {
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
		return OpenFileInEditor{
			Path:      path,
			Workspace: ws,
		}
	}
}

// OpenFileInEditor is a message to open a file in the editor
type OpenFileInEditor struct {
	Path      string
	Workspace *data.Workspace
}

// expandNode loads children for a directory node
func (m *ProjectTree) expandNode(node *ProjectTreeNode) {
	if !node.IsDir || node.Expanded {
		return
	}

	entries, err := os.ReadDir(node.Path)
	if err != nil {
		return
	}

	node.Children = nil
	var dirs, files []*ProjectTreeNode

	for _, entry := range entries {
		name := entry.Name()
		if !m.showHidden && strings.HasPrefix(name, ".") {
			continue
		}

		child := &ProjectTreeNode{
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

	var walk func(node *ProjectTreeNode)
	walk = func(node *ProjectTreeNode) {
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

// reloadTree reloads the entire tree from disk
func (m *ProjectTree) reloadTree() {
	if m.workspace == nil {
		m.root = nil
		m.flatNodes = nil
		return
	}

	m.root = &ProjectTreeNode{
		Name:   filepath.Base(m.workspace.Root),
		Path:   m.workspace.Root,
		IsDir:  true,
		Depth:  -1, // Root is at depth -1 so children are at 0
		Parent: nil,
	}

	m.expandNode(m.root)
	m.rebuildFlatList()
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
	m.width = width
	m.height = height
}

// Focus sets the focus state
func (m *ProjectTree) Focus() {
	m.focused = true
}

// Blur removes focus
func (m *ProjectTree) Blur() {
	m.focused = false
}

// Focused returns whether the tree is focused
func (m *ProjectTree) Focused() bool {
	return m.focused
}

// SetWorkspace sets the active workspace
func (m *ProjectTree) SetWorkspace(ws *data.Workspace) {
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

func (m *ProjectTree) rebaseTreePaths(oldRoot, newRoot string) bool {
	if m.root == nil {
		return false
	}
	oldClean := filepath.Clean(strings.TrimSpace(oldRoot))
	newClean := filepath.Clean(strings.TrimSpace(newRoot))
	if oldClean == "" || newClean == "" {
		return false
	}

	var walk func(*ProjectTreeNode) bool
	walk = func(node *ProjectTreeNode) bool {
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
