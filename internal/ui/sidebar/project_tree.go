package sidebar

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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
	worktree     *data.Worktree
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
		showHidden: false,
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
	wt := m.worktree
	path := node.Path
	return func() tea.Msg {
		return OpenFileInEditor{
			Path:     path,
			Worktree: wt,
		}
	}
}

// OpenFileInEditor is a message to open a file in the editor
type OpenFileInEditor struct {
	Path     string
	Worktree *data.Worktree
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
	if m.worktree == nil {
		m.root = nil
		m.flatNodes = nil
		return
	}

	m.root = &ProjectTreeNode{
		Name:   filepath.Base(m.worktree.Root),
		Path:   m.worktree.Root,
		IsDir:  true,
		Depth:  -1, // Root is at depth -1 so children are at 0
		Parent: nil,
	}

	m.expandNode(m.root)
	m.rebuildFlatList()
}

// View renders the project tree
func (m *ProjectTree) View() string {
	if m.worktree == nil {
		return m.renderWithHelp(m.styles.Muted.Render("No worktree selected"))
	}

	if len(m.flatNodes) == 0 {
		return m.renderWithHelp(m.styles.Muted.Render("Empty directory"))
	}

	var b strings.Builder
	visibleHeight := m.visibleHeight()

	// Adjust scroll
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+visibleHeight {
		m.scrollOffset = m.cursor - visibleHeight + 1
	}

	for i, node := range m.flatNodes {
		if i < m.scrollOffset {
			continue
		}
		if i >= m.scrollOffset+visibleHeight {
			break
		}

		// Cursor indicator
		cursor := common.Icons.CursorEmpty + " "
		if i == m.cursor {
			cursor = common.Icons.Cursor + " "
		}

		// Indentation
		indent := strings.Repeat("  ", node.Depth)

		// Icon
		var icon string
		if node.IsDir {
			if node.Expanded {
				icon = common.Icons.DirOpen + " "
			} else {
				icon = common.Icons.DirClosed + " "
			}
		} else {
			icon = common.Icons.File + " "
		}

		// Name with styling
		name := node.Name
		maxNameWidth := m.width - lipgloss.Width(cursor+indent+icon) - 1
		if maxNameWidth < 5 {
			maxNameWidth = 5
		}
		if lipgloss.Width(name) > maxNameWidth {
			runes := []rune(name)
			for len(runes) > 4 && lipgloss.Width(string(runes)) > maxNameWidth-3 {
				runes = runes[:len(runes)-1]
			}
			name = string(runes) + "..."
		}

		var nameStyled string
		if node.IsDir {
			nameStyled = m.styles.DirName.Render(name)
		} else {
			nameStyled = m.styles.FilePath.Render(name)
		}

		line := cursor + indent + icon + nameStyled
		b.WriteString(line + "\n")
	}

	content := b.String()
	if len(content) > 0 && content[len(content)-1] == '\n' {
		content = content[:len(content)-1]
	}

	return m.renderWithHelp(content)
}

func (m *ProjectTree) renderWithHelp(content string) string {
	// Help bar
	contentWidth := m.width
	if contentWidth < 1 {
		contentWidth = 1
	}
	helpLines := m.helpLines(contentWidth)
	if !m.showKeymapHints {
		helpLines = nil
	}

	contentHeight := 0
	if content != "" {
		contentHeight = strings.Count(content, "\n") + 1
	}

	targetHeight := m.height - len(helpLines)
	if targetHeight < 0 {
		targetHeight = 0
	}

	var b strings.Builder
	b.WriteString(content)
	if targetHeight > contentHeight {
		b.WriteString(strings.Repeat("\n", targetHeight-contentHeight))
	}
	if len(helpLines) > 0 {
		if content != "" && targetHeight == contentHeight {
			b.WriteString("\n")
		}
		b.WriteString(strings.Join(helpLines, "\n"))
	}

	// Ensure output doesn't exceed m.height lines
	result := b.String()
	if m.height > 0 {
		lines := strings.Split(result, "\n")
		if len(lines) > m.height {
			lines = lines[:m.height]
			result = strings.Join(lines, "\n")
		}
	}
	return result
}

func (m *ProjectTree) helpItem(key, desc string) string {
	return common.RenderHelpItem(m.styles, key, desc)
}

func (m *ProjectTree) helpLines(contentWidth int) []string {
	items := []string{
		m.helpItem("k/↑", "up"),
		m.helpItem("j/↓", "down"),
		m.helpItem("h/←", "collapse"),
		m.helpItem("l/→", "expand"),
		m.helpItem(".", "hidden"),
		m.helpItem("r", "refresh"),
	}
	return common.WrapHelpItems(items, contentWidth)
}

func (m *ProjectTree) helpLineCount() int {
	if !m.showKeymapHints {
		return 0
	}
	contentWidth := m.width
	if contentWidth < 1 {
		contentWidth = 1
	}
	return len(m.helpLines(contentWidth))
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

// SetWorktree sets the active worktree
func (m *ProjectTree) SetWorktree(wt *data.Worktree) {
	m.worktree = wt
	m.cursor = 0
	m.scrollOffset = 0
	m.reloadTree()
}
