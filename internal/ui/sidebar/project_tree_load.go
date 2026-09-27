package sidebar

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// projectTreeMaxConcurrentLoads bounds how many os.ReadDir calls one tree may
// have in flight. A kernel-blocked read occupies its slot until the OS
// returns — it cannot be canceled — so the bound keeps refresh storms and
// workspace churn from spawning unbounded directory I/O.
const projectTreeMaxConcurrentLoads = 4

// ProjectTreeEntry is one immutable directory-listing record carried by a
// load result; the Update goroutine converts these into tree nodes.
type ProjectTreeEntry struct {
	Name  string
	IsDir bool
}

// ProjectTreeDirectoryLoaded is the immutable result of one asynchronous
// directory read. Generation and RequestID are the staleness proof: the model
// applies a result only while the tree's generation and the node's pending
// request identity still match the job that was issued.
type ProjectTreeDirectoryLoaded struct {
	Generation uint64
	RequestID  uint64
	Path       string
	Entries    []ProjectTreeEntry
	Err        error
}

// projectTreeLoadJob is scheduler bookkeeping. The node pointer is Update-side
// state only — the returned command captures the path and the readDir
// function snapshot, never a mutable node or the model.
type projectTreeLoadJob struct {
	requestID  uint64
	generation uint64
	path       string
	node       *projectTreeNode
}

// defaultReadDir is the per-model seam over os.ReadDir; tests substitute a
// barrier or failure fixture on the model, never a package global.
func defaultReadDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}

// enqueueLoad registers a directory read for node and returns whatever work
// the bounded scheduler can dispatch now. Repeated requests for a node that
// already has one pending are deduplicated.
func (m *ProjectTree) enqueueLoad(node *projectTreeNode) tea.Cmd {
	if node == nil || !node.IsDir || node.pendingRequest != 0 {
		return nil
	}
	m.nextRequestID++
	node.pendingRequest = m.nextRequestID
	m.loadQueue = append(m.loadQueue, projectTreeLoadJob{
		requestID:  node.pendingRequest,
		generation: m.generation,
		path:       node.Path,
		node:       node,
	})
	return m.scheduleLoads()
}

// scheduleLoads moves eligible queued jobs into the executing set (up to the
// cap) and returns their commands batched. Superseded queued jobs — wrong
// generation, or a node whose pending request was invalidated — are dropped
// without ever running.
func (m *ProjectTree) scheduleLoads() tea.Cmd {
	var cmds []tea.Cmd
	for len(m.executing) < projectTreeMaxConcurrentLoads && len(m.loadQueue) > 0 {
		job := m.loadQueue[0]
		m.loadQueue = m.loadQueue[1:]
		if job.generation != m.generation || job.node == nil || job.node.pendingRequest != job.requestID {
			continue // superseded before it ever ran
		}
		m.executing[job.requestID] = job
		cmds = append(cmds, m.issueLoad(job))
	}
	if len(cmds) == 0 {
		return nil
	}
	return common.SafeBatch(cmds...)
}

// issueLoad snapshots the readDir function and job identity into the command.
// The command reads no model state, so it is safe to execute off the Update
// goroutine.
func (m *ProjectTree) issueLoad(job projectTreeLoadJob) tea.Cmd {
	readDir := m.readDir
	return func() tea.Msg {
		entries, err := readDir(job.path)
		msg := ProjectTreeDirectoryLoaded{
			Generation: job.generation,
			RequestID:  job.requestID,
			Path:       job.path,
			Err:        err,
		}
		for _, e := range entries {
			msg.Entries = append(msg.Entries, ProjectTreeEntry{Name: e.Name(), IsDir: e.IsDir()})
		}
		return msg
	}
}

// applyDirectoryLoaded handles a completion on the Update goroutine. A stale
// completion releases its executing slot and schedules queued work, but may
// not mutate any node.
func (m *ProjectTree) applyDirectoryLoaded(msg ProjectTreeDirectoryLoaded) tea.Cmd {
	job, ok := m.executing[msg.RequestID]
	if !ok {
		return nil // foreign or already-forgotten result
	}
	delete(m.executing, msg.RequestID)
	followup := m.scheduleLoads()

	if msg.Generation != m.generation || job.node == nil || job.node.pendingRequest != msg.RequestID {
		return followup
	}
	node := job.node
	node.pendingRequest = 0

	if node == m.pendingRoot {
		return common.SafeBatch(followup, m.applyRootResult(msg))
	}

	// A node the user collapsed while the read ran no longer wants expansion.
	if !node.Expanded {
		return followup
	}

	if msg.Err != nil {
		node.loadErr = sanitizeTreeError(msg.Err)
		m.markContentDirty()
		return followup
	}
	node.loadErr = ""
	m.installChildren(node, msg.Entries)
	restored := m.restorePendingExpansions(node)
	m.rebuildFlatList()
	m.restorePendingSelection()
	m.markContentDirty()
	return common.SafeBatch(followup, restored)
}

// applyRootResult swaps a refreshed root (loaded while the old tree stayed
// visible) into place, or surfaces the error on the retained old tree.
func (m *ProjectTree) applyRootResult(msg ProjectTreeDirectoryLoaded) tea.Cmd {
	root := m.pendingRoot
	m.pendingRoot = nil
	if msg.Err != nil {
		m.rootErr = sanitizeTreeError(msg.Err)
		m.markContentDirty()
		return nil
	}
	m.rootErr = ""
	m.root = root
	m.installChildren(root, msg.Entries)
	restored := m.restorePendingExpansions(root)
	m.rebuildFlatList()
	m.restorePendingSelection()
	m.markContentDirty()
	return restored
}

// restorePendingExpansions queues loads for children of node whose paths were
// expanded before the current reload began, returning their dispatched
// commands.
func (m *ProjectTree) restorePendingExpansions(node *projectTreeNode) tea.Cmd {
	if len(m.pendingExpanded) == 0 {
		return nil
	}
	var cmd tea.Cmd
	for _, child := range node.Children {
		if child.IsDir && m.pendingExpanded[child.Path] {
			delete(m.pendingExpanded, child.Path)
			child.Expanded = true
			cmd = common.SafeBatch(cmd, m.enqueueLoad(child))
		}
	}
	return cmd
}

// sanitizeTreeError trims a directory-read error to a short display-safe
// hint; full detail stays in logs territory, the marker only needs the shape.
func sanitizeTreeError(err error) string {
	s := common.SanitizeDisplayText(err.Error(), 64)
	s = strings.TrimSpace(s)
	if s == "" {
		return "load failed"
	}
	return s
}

// restorePendingSelection puts the cursor back on the pre-refresh path unless
// the user navigated in the meantime; a vanished target leaves the flat-list
// clamp in charge (nearest valid row).
func (m *ProjectTree) restorePendingSelection() {
	if m.pendingSelectedPath == "" || m.navVersion != m.refreshNavVersion {
		return
	}
	for i, node := range m.flatNodes {
		if node.Path == m.pendingSelectedPath {
			m.cursor = i
			m.pendingSelectedPath = ""
			return
		}
	}
	// Not found yet: more restorations may still be in flight. Give up only
	// when nothing else can arrive — remaining remembered paths belong to
	// deleted directories, so drop them and let the cursor clamp.
	if len(m.executing) == 0 && len(m.loadQueue) == 0 {
		m.pendingExpanded = nil
		m.pendingSelectedPath = ""
	}
}

// reloadTree refreshes the whole tree from disk. The current visible tree is
// retained while a new root loads into pendingRoot; when that result lands,
// expansion and selection intent are restored incrementally as each
// remembered directory's listing arrives.
func (m *ProjectTree) reloadTree() tea.Cmd {
	if m.workspace == nil {
		m.invalidatePending()
		m.root = nil
		m.flatNodes = nil
		return nil
	}

	m.invalidatePending()
	m.pendingExpanded = m.collectExpandedPaths()
	m.refreshNavVersion = m.navVersion
	m.pendingSelectedPath = ""
	if m.cursor >= 0 && m.cursor < len(m.flatNodes) {
		m.pendingSelectedPath = m.flatNodes[m.cursor].Path
	}

	m.pendingRoot = &projectTreeNode{
		Name:     filepath.Base(m.workspace.Root),
		Path:     m.workspace.Root,
		IsDir:    true,
		Expanded: true,
		Depth:    -1, // Root is at depth -1 so children are at 0
		Parent:   nil,
	}
	m.rootErr = ""
	return m.enqueueLoad(m.pendingRoot)
}

// invalidatePending bumps the generation, clears the queue, and resets every
// node's pending token so queued superseded work drops and in-flight results
// are discarded on arrival. Executing jobs keep their bounded slots until
// they finish — a kernel-blocked ReadDir cannot be canceled.
func (m *ProjectTree) invalidatePending() {
	m.generation++
	m.loadQueue = nil
	m.pendingRoot = nil
	m.pendingExpanded = nil
	m.pendingSelectedPath = ""
	m.executing = map[uint64]projectTreeLoadJob{}
	if m.root != nil {
		var clearPending func(n *projectTreeNode)
		clearPending = func(n *projectTreeNode) {
			n.pendingRequest = 0
			for _, child := range n.Children {
				clearPending(child)
			}
		}
		clearPending(m.root)
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
		if n.IsDir && n.Expanded && n != m.root {
			expanded[n.Path] = true
		}
		for _, child := range n.Children {
			collect(child)
		}
	}
	collect(m.root)
	return expanded
}

// SetWorkspace sets the active workspace. It returns the command that kicks
// off the root directory load (or re-request after a root path rebase);
// callers must route it for the tree to ever populate.
func (m *ProjectTree) SetWorkspace(ws *data.Workspace) tea.Cmd {
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
				return m.reloadTree()
			}
			return m.reRequestRebasedTree()
		}
		return nil
	}
	m.workspace = ws
	m.cursor = 0
	m.scrollOffset = 0
	m.invalidatePending()
	m.rootErr = ""
	if ws == nil {
		m.root = nil
		m.flatNodes = nil
		return nil
	}
	m.root = &projectTreeNode{
		Name:     filepath.Base(ws.Root),
		Path:     ws.Root,
		IsDir:    true,
		Expanded: true,
		Depth:    -1,
	}
	m.rebuildFlatList()
	return m.enqueueLoad(m.root)
}

// reRequestRebasedTree runs after a successful path rebase: node contents are
// still accurate (same directories, new spelling), so only in-flight requests
// are invalid. Any node whose read was pending is re-requested under the new
// generation so its loading marker resolves.
func (m *ProjectTree) reRequestRebasedTree() tea.Cmd {
	m.generation++
	m.loadQueue = nil
	m.executing = map[uint64]projectTreeLoadJob{}
	var pending []*projectTreeNode
	var walk func(n *projectTreeNode)
	walk = func(n *projectTreeNode) {
		if n.pendingRequest != 0 {
			n.pendingRequest = 0
			pending = append(pending, n)
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	if m.root != nil {
		walk(m.root)
	}
	if m.pendingRoot != nil {
		m.pendingRoot.pendingRequest = 0
		pending = append(pending, m.pendingRoot)
		m.pendingRoot = nil
	}
	var cmd tea.Cmd
	for _, node := range pending {
		cmd = common.SafeBatch(cmd, m.enqueueLoad(node))
	}
	return cmd
}
