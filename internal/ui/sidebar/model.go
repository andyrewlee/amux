package sidebar

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// TabType identifies the sidebar tab
type TabType int

const (
	TabChanges TabType = iota
	TabExplorer
)

// FileEntry represents a file in the explorer
type FileEntry struct {
	Path     string
	Name     string
	IsDir    bool
	Expanded bool
	Depth    int
}

// Model is the Bubbletea model for the sidebar pane
type Model struct {
	// State
	worktree      *data.Worktree
	activeTab     TabType
	focused       bool
	gitStatus     *git.StatusResult
	expandedDirs  map[string]bool
	explorerFiles []FileEntry
	cursor        int
	scrollOffset  int

	// Layout
	width  int
	height int

	// Styles
	styles common.Styles
}

// New creates a new sidebar model
func New() *Model {
	return &Model{
		activeTab:    TabChanges,
		expandedDirs: make(map[string]bool),
		styles:       common.DefaultStyles(),
	}
}

// Init initializes the sidebar
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !m.focused {
			return m, nil
		}

		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("1"))):
			m.activeTab = TabChanges
			m.cursor = 0
		case key.Matches(msg, key.NewBinding(key.WithKeys("2"))):
			m.activeTab = TabExplorer
			m.cursor = 0
			if m.worktree != nil {
				m.loadExplorer()
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			m.moveCursor(1)
		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			m.moveCursor(-1)
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter", "tab"))):
			m.handleEnter()
		case key.Matches(msg, key.NewBinding(key.WithKeys("g"))):
			cmds = append(cmds, m.refreshStatus())
		}
	}

	return m, tea.Batch(cmds...)
}

// View renders the sidebar
func (m *Model) View() string {
	var b strings.Builder

	// Tab bar
	b.WriteString(m.renderTabBar())
	b.WriteString("\n\n")

	// Content based on active tab
	switch m.activeTab {
	case TabChanges:
		b.WriteString(m.renderChanges())
	case TabExplorer:
		b.WriteString(m.renderExplorer())
	}

	// Help bar with styled keys
	helpItems := []string{
		m.styles.HelpKey.Render("1/2") + m.styles.HelpDesc.Render(":tabs"),
		m.styles.HelpKey.Render("j/k") + m.styles.HelpDesc.Render(":nav"),
		m.styles.HelpKey.Render("g") + m.styles.HelpDesc.Render(":refresh"),
	}
	help := strings.Join(helpItems, "  ")
	// Account for: border (2 lines) + help line (1)
	contentHeight := strings.Count(b.String(), "\n") + 1
	targetHeight := m.height - 4 // 2 for border, 1 for help, 1 for safety
	if targetHeight > contentHeight {
		b.WriteString(strings.Repeat("\n", targetHeight-contentHeight))
	}
	b.WriteString(help)

	// Apply pane styling
	style := m.styles.Pane
	if m.focused {
		style = m.styles.FocusedPane
	}

	return style.Width(m.width - 2).Render(b.String())
}

// renderTabBar renders the sidebar tab bar
func (m *Model) renderTabBar() string {
	tabs := []struct {
		name   string
		active bool
	}{
		{"changes", m.activeTab == TabChanges},
		{"files", m.activeTab == TabExplorer},
	}

	var parts []string
	for _, tab := range tabs {
		style := m.styles.Tab
		if tab.active {
			style = m.styles.ActiveTab
		}
		parts = append(parts, style.Render(tab.name))
	}

	return m.styles.TabBar.Render(strings.Join(parts, " "))
}

// renderChanges renders the git changes tab
func (m *Model) renderChanges() string {
	if m.gitStatus == nil {
		return m.styles.Muted.Render("No status loaded")
	}

	var b strings.Builder

	// Show branch info if available
	if m.worktree != nil && m.worktree.Branch != "" {
		b.WriteString(m.styles.Muted.Render("branch: "))
		b.WriteString(m.styles.BranchName.Render(m.worktree.Branch))
		b.WriteString("\n")
	}

	if m.gitStatus.Clean {
		b.WriteString("\n")
		b.WriteString(m.styles.StatusClean.Render(common.Icons.Clean + " Working tree clean"))
		return b.String()
	}

	// Show file count
	b.WriteString(m.styles.Muted.Render(strconv.Itoa(len(m.gitStatus.Files)) + " changed files"))
	b.WriteString("\n\n")

	visibleHeight := m.height - 10
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	// Adjust scroll
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+visibleHeight {
		m.scrollOffset = m.cursor - visibleHeight + 1
	}

	for i, file := range m.gitStatus.Files {
		if i < m.scrollOffset {
			continue
		}
		if i >= m.scrollOffset+visibleHeight {
			break
		}

		cursor := common.Icons.CursorEmpty + " "
		if i == m.cursor && m.activeTab == TabChanges {
			cursor = common.Icons.Cursor + " "
		}

		// Status indicator with color
		var statusStyle lipgloss.Style
		switch {
		case file.IsModified():
			statusStyle = m.styles.StatusModified
		case file.IsAdded():
			statusStyle = m.styles.StatusAdded
		case file.IsDeleted():
			statusStyle = m.styles.StatusDeleted
		case file.IsUntracked():
			statusStyle = m.styles.StatusUntracked
		default:
			statusStyle = m.styles.Muted
		}

		// Truncate path to fit within available width
		// Account for: cursor (2) + code (2) + spaces (2) + padding (4) = 10 chars overhead
		maxPathLen := m.width - 14
		if maxPathLen < 10 {
			maxPathLen = 10
		}
		displayPath := file.Path
		if len(displayPath) > maxPathLen {
			displayPath = "..." + displayPath[len(displayPath)-maxPathLen+3:]
		}

		line := cursor + statusStyle.Render(file.Code) + " " + m.styles.FilePath.Render(displayPath)
		b.WriteString(line + "\n")
	}

	return b.String()
}

// renderExplorer renders the file explorer tab
func (m *Model) renderExplorer() string {
	if m.worktree == nil {
		return m.styles.Muted.Render("No worktree selected")
	}

	if len(m.explorerFiles) == 0 {
		m.loadExplorer()
	}

	var b strings.Builder
	visibleHeight := m.height - 8
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	// Adjust scroll
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+visibleHeight {
		m.scrollOffset = m.cursor - visibleHeight + 1
	}

	for i, entry := range m.explorerFiles {
		if i < m.scrollOffset {
			continue
		}
		if i >= m.scrollOffset+visibleHeight {
			break
		}

		cursor := common.Icons.CursorEmpty + " "
		if i == m.cursor && m.activeTab == TabExplorer {
			cursor = common.Icons.Cursor + " "
		}

		indent := strings.Repeat("  ", entry.Depth)

		var icon string
		var style lipgloss.Style
		if entry.IsDir {
			if entry.Expanded {
				icon = common.Icons.ArrowDown + " "
			} else {
				icon = common.Icons.ArrowRight + " "
			}
			style = m.styles.BranchName // Use purple for directories
		} else {
			icon = common.Icons.File + " "
			style = m.styles.FilePath
		}

		line := cursor + indent + style.Render(icon+entry.Name)
		b.WriteString(line + "\n")
	}

	return b.String()
}

// loadExplorer loads the file explorer entries
func (m *Model) loadExplorer() {
	if m.worktree == nil {
		return
	}

	m.explorerFiles = []FileEntry{}
	m.loadDir(m.worktree.Root, 0)
}

// maxExplorerDepth is the maximum depth for file explorer
const maxExplorerDepth = 10

// loadDir recursively loads directory contents
func (m *Model) loadDir(path string, depth int) {
	// Limit depth to prevent runaway recursion
	if depth >= maxExplorerDepth {
		return
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return
	}

	// Limit number of entries per directory to prevent UI overload
	const maxEntriesPerDir = 100
	if len(entries) > maxEntriesPerDir {
		entries = entries[:maxEntriesPerDir]
	}

	// Sort: directories first, then alphabetically
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden and ignored
		if strings.HasPrefix(name, ".") {
			continue
		}
		if name == "node_modules" || name == "__pycache__" || name == "vendor" || name == "dist" || name == "build" || name == ".git" {
			continue
		}

		fullPath := filepath.Join(path, name)
		relPath, _ := filepath.Rel(m.worktree.Root, fullPath)

		fe := FileEntry{
			Path:     relPath,
			Name:     name,
			IsDir:    entry.IsDir(),
			Expanded: m.expandedDirs[relPath],
			Depth:    depth,
		}

		m.explorerFiles = append(m.explorerFiles, fe)

		// Recursively load expanded directories
		if entry.IsDir() && fe.Expanded {
			m.loadDir(fullPath, depth+1)
		}
	}
}

// handleEnter handles the enter key
func (m *Model) handleEnter() {
	switch m.activeTab {
	case TabChanges:
		// Could open diff view
	case TabExplorer:
		if m.cursor < len(m.explorerFiles) {
			entry := &m.explorerFiles[m.cursor]
			if entry.IsDir {
				m.expandedDirs[entry.Path] = !m.expandedDirs[entry.Path]
				m.loadExplorer()
			}
		}
	}
}

// moveCursor moves the cursor
func (m *Model) moveCursor(delta int) {
	var maxLen int
	switch m.activeTab {
	case TabChanges:
		if m.gitStatus != nil {
			maxLen = len(m.gitStatus.Files)
		}
	case TabExplorer:
		maxLen = len(m.explorerFiles)
	}

	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= maxLen {
		m.cursor = maxLen - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// refreshStatus refreshes the git status
func (m *Model) refreshStatus() tea.Cmd {
	if m.worktree == nil {
		return nil
	}

	root := m.worktree.Root
	return func() tea.Msg {
		status, err := git.GetStatus(root)
		return messages.GitStatusResult{Root: root, Status: status, Err: err}
	}
}

// SetSize sets the sidebar size
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Focus sets the focus state
func (m *Model) Focus() {
	m.focused = true
}

// Blur removes focus
func (m *Model) Blur() {
	m.focused = false
}

// Focused returns whether the sidebar is focused
func (m *Model) Focused() bool {
	return m.focused
}

// SetWorktree sets the active worktree
func (m *Model) SetWorktree(wt *data.Worktree) {
	m.worktree = wt
	m.cursor = 0
	m.scrollOffset = 0
	m.explorerFiles = nil
	m.expandedDirs = make(map[string]bool)
}

// SetGitStatus sets the git status
func (m *Model) SetGitStatus(status *git.StatusResult) {
	m.gitStatus = status
}
