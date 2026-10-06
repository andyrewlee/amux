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

	// pendingRequest is the in-flight directory read for this node (0 =
	// none). It is both the loading marker and the staleness token: a result
	// applies only while it still matches the delivered RequestID.
	pendingRequest uint64
	// loadErr is a sanitized, generation-scoped load failure marker rendered
	// under the node; empty means none.
	loadErr string
}

// ProjectTree is a nerdtree-like file browser. All directory I/O happens in
// returned commands producing ProjectTreeDirectoryLoaded; Update owns every
// node mutation.
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

	// Async directory loading (see project_tree_load.go):
	// generation invalidates every outstanding job on workspace switch,
	// refresh, or root rebase; executing holds running jobs (the
	// per-generation admission cap's source of truth) keyed by request ID;
	// loadQueue is FIFO pending work; pendingRoot is the shadow root a
	// refresh is loading while the old tree stays visible.
	// readSlots is the PHYSICAL bound: a channel semaphore held by every
	// dispatched ReadDir until the syscall returns — a separate budget from
	// the generation-scoped executing set, which invalidation clears while
	// kernel-blocked reads keep running.
	generation    uint64
	nextRequestID uint64
	executing     map[uint64]projectTreeLoadJob
	loadQueue     []projectTreeLoadJob
	pendingRoot   *projectTreeNode
	rootErr       string
	readSlots     chan struct{}
	// pendingExpanded/pendingSelectedPath carry expansion and selection
	// intent across an in-flight reload; navVersion records the cursor
	// movement counter at refresh start so a user navigation while loading
	// keeps the result from yanking selection back.
	pendingExpanded     map[string]bool
	pendingSelectedPath string
	navVersion          uint64
	refreshNavVersion   uint64

	// readDir is the per-model filesystem seam (default os.ReadDir),
	// snapshotted into each issued command.
	readDir func(string) ([]os.DirEntry, error)

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
		executing:  map[uint64]projectTreeLoadJob{},
		readSlots:  make(chan struct{}, projectTreeMaxConcurrentLoads),
		readDir:    defaultReadDir,
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

	// Directory load results land regardless of focus: they are async
	// responses, not input, and hiding them behind the focus guard would
	// strand in-flight reads when the user blurs mid-load.
	if msg, ok := msg.(ProjectTreeDirectoryLoaded); ok {
		return m, m.applyDirectoryLoaded(msg)
	}

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
			m.navVersion++
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
					return m, m.expandNode(node)
				}
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("h", "left"))):
			// Collapse directory or go to parent
			if m.cursor >= 0 && m.cursor < len(m.flatNodes) {
				node := m.flatNodes[m.cursor]
				if node.IsDir && node.Expanded {
					m.collapseNode(node)
				} else if node.Parent != nil {
					// Find and move to parent
					for i, n := range m.flatNodes {
						if n == node.Parent {
							m.cursor = i
							m.navVersion++
							break
						}
					}
				}
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("."))):
			// Toggle hidden files
			m.showHidden = !m.showHidden
			return m, m.reloadTree()
		case key.Matches(msg, key.NewBinding(key.WithKeys("r"))):
			// Refresh tree
			return m, m.reloadTree()
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
			m.collapseNode(node)
			return nil
		}
		return m.expandNode(node)
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

// expandNode marks a directory open and queues the asynchronous read that
// fills its children. The row reflects the expanded/loading state
// immediately; children appear when the result applies.
func (m *ProjectTree) expandNode(node *projectTreeNode) tea.Cmd {
	if !node.IsDir || node.Expanded {
		return nil
	}
	node.Expanded = true
	node.loadErr = ""
	m.rebuildFlatList()
	return m.enqueueLoad(node)
}

// collapseNode closes a directory and invalidates every pending request under
// it: queued reads for hidden descendants are dropped by the scheduler, while
// reads already running keep their job IDs (and a bounded slot) until their
// results arrive and are discarded as stale.
func (m *ProjectTree) collapseNode(node *projectTreeNode) {
	node.Expanded = false
	node.loadErr = ""
	var invalidate func(n *projectTreeNode)
	invalidate = func(n *projectTreeNode) {
		n.pendingRequest = 0
		for _, child := range n.Children {
			invalidate(child)
		}
	}
	invalidate(node)
	m.rebuildFlatList()
}

// installChildren converts an immutable load result into child nodes on the
// Update goroutine — hidden-file filtering and the directory-first,
// case-insensitive sort happen here so a result can never reorder the tree
// out from under the reader.
func (m *ProjectTree) installChildren(node *projectTreeNode, entries []ProjectTreeEntry) {
	var dirs, files []*projectTreeNode
	for _, e := range entries {
		if !m.showHidden && strings.HasPrefix(e.Name, ".") {
			continue
		}
		child := &projectTreeNode{
			Name:   e.Name,
			Path:   filepath.Join(node.Path, e.Name),
			IsDir:  e.IsDir,
			Depth:  node.Depth + 1,
			Parent: node,
		}
		if e.IsDir {
			dirs = append(dirs, child)
		} else {
			files = append(files, child)
		}
	}

	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})
	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})

	node.Children = append(dirs, files...)
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
	m.navVersion++

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
