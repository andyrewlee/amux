package center

import (
	"context"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/vterm"
)

// MonitorSnapshot captures a tab display for the monitor grid.
type MonitorSnapshot struct {
	Worktree  *data.Worktree
	Assistant string
	Name      string
	Running   bool
	Rendered  string
}

// MonitorTab describes a tab for the monitor grid.
type MonitorTab struct {
	ID        TabID
	Worktree  *data.Worktree
	Assistant string
	Name      string
	Running   bool
}

// TabSize defines a desired size for a tab.
type TabSize struct {
	ID     TabID
	Width  int
	Height int
}

// MonitorTabSnapshot captures a monitor tab with its visible screen.
type MonitorTabSnapshot struct {
	MonitorTab
	Screen     [][]vterm.Cell
	CursorX    int
	CursorY    int
	ViewOffset int
	Width      int
	Height     int
	SelActive  bool
	SelStartX  int
	SelStartY  int
	SelEndX    int
	SelEndY    int
}

// HandleMonitorInput forwards input to a specific tab while in monitor view.
func (m *Model) HandleMonitorInput(tabID TabID, msg tea.Msg) tea.Cmd {
	tab := m.getTabByIDGlobal(tabID)
	if tab == nil || tab.isClosed() || tab.Agent == nil || tab.Agent.Terminal == nil {
		return nil
	}
	wtID := ""
	if tab.Worktree != nil {
		wtID = string(tab.Worktree.ID())
	}

	switch msg := msg.(type) {
	case tea.PasteMsg:
		// Handle bracketed paste - send entire content at once with escape sequences.
		if m.isTabActorReady() {
			if !m.sendTabEvent(tabEvent{
				tab:        tab,
				worktreeID: wtID,
				tabID:      tab.ID,
				kind:       tabEventPaste,
				pasteText:  msg.Content,
			}) {
				bracketedText := "\x1b[200~" + msg.Content + "\x1b[201~"
				_ = tab.Agent.Terminal.SendString(bracketedText)
			}
		} else {
			bracketedText := "\x1b[200~" + msg.Content + "\x1b[201~"
			_ = tab.Agent.Terminal.SendString(bracketedText)
		}
		return nil

	case tea.KeyPressMsg:
		switch {
		case msg.Key().Code == tea.KeyPgUp:
			if m.isTabActorReady() {
				if m.sendTabEvent(tabEvent{
					tab:        tab,
					worktreeID: wtID,
					tabID:      tab.ID,
					kind:       tabEventScrollPage,
					scrollPage: 1,
				}) {
					return nil
				}
			}
			{
				tab.mu.Lock()
				if tab.Terminal != nil {
					tab.Terminal.ScrollView(tab.Terminal.Height / 4)
					tab.monitorDirty = true
				}
				tab.mu.Unlock()
			}
			return nil

		case msg.Key().Code == tea.KeyPgDown:
			if m.isTabActorReady() {
				if m.sendTabEvent(tabEvent{
					tab:        tab,
					worktreeID: wtID,
					tabID:      tab.ID,
					kind:       tabEventScrollPage,
					scrollPage: -1,
				}) {
					return nil
				}
			}
			{
				tab.mu.Lock()
				if tab.Terminal != nil {
					tab.Terminal.ScrollView(-tab.Terminal.Height / 4)
					tab.monitorDirty = true
				}
				tab.mu.Unlock()
			}
			return nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+u"))):
			if m.isTabActorReady() {
				if m.sendTabEvent(tabEvent{
					tab:        tab,
					worktreeID: wtID,
					tabID:      tab.ID,
					kind:       tabEventScrollPage,
					scrollPage: 1,
				}) {
					return nil
				}
			}
			{
				tab.mu.Lock()
				if tab.Terminal != nil {
					tab.Terminal.ScrollView(tab.Terminal.Height / 4)
					tab.monitorDirty = true
				}
				tab.mu.Unlock()
			}
			return nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+d"))):
			if m.isTabActorReady() {
				if m.sendTabEvent(tabEvent{
					tab:        tab,
					worktreeID: wtID,
					tabID:      tab.ID,
					kind:       tabEventScrollPage,
					scrollPage: -1,
				}) {
					return nil
				}
			}
			{
				tab.mu.Lock()
				if tab.Terminal != nil {
					tab.Terminal.ScrollView(-tab.Terminal.Height / 4)
					tab.monitorDirty = true
				}
				tab.mu.Unlock()
			}
			return nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("home"))):
			if m.isTabActorReady() {
				if m.sendTabEvent(tabEvent{
					tab:        tab,
					worktreeID: wtID,
					tabID:      tab.ID,
					kind:       tabEventScrollToTop,
				}) {
					return nil
				}
			}
			{
				tab.mu.Lock()
				if tab.Terminal != nil {
					tab.Terminal.ScrollViewToTop()
					tab.monitorDirty = true
				}
				tab.mu.Unlock()
			}
			return nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("end"))):
			if m.isTabActorReady() {
				if m.sendTabEvent(tabEvent{
					tab:        tab,
					worktreeID: wtID,
					tabID:      tab.ID,
					kind:       tabEventScrollToBottom,
				}) {
					return nil
				}
			}
			{
				tab.mu.Lock()
				if tab.Terminal != nil {
					tab.Terminal.ScrollViewToBottom()
					tab.monitorDirty = true
				}
				tab.mu.Unlock()
			}
			return nil
		}

		// If scrolled, any typing goes back to live and sends key.
		sent := false
		if m.isTabActorReady() {
			sent = m.sendTabEvent(tabEvent{
				tab:        tab,
				worktreeID: wtID,
				tabID:      tab.ID,
				kind:       tabEventScrollToBottom,
			})
		}
		if !sent {
			tab.mu.Lock()
			if tab.Terminal != nil && tab.Terminal.IsScrolled() {
				tab.Terminal.ScrollViewToBottom()
				tab.monitorDirty = true
			}
			tab.mu.Unlock()
		}

		input := common.KeyToBytes(msg)
		if len(input) > 0 {
			if m.isTabActorReady() {
				if !m.sendTabEvent(tabEvent{
					tab:        tab,
					worktreeID: wtID,
					tabID:      tab.ID,
					kind:       tabEventSendInput,
					input:      input,
				}) {
					_ = tab.Agent.Terminal.SendString(string(input))
				}
			} else {
				_ = tab.Agent.Terminal.SendString(string(input))
			}
		}
	}

	return nil
}

// MonitorSnapshots returns a snapshot of each tab for the monitor grid.
func (m *Model) MonitorSnapshots() []MonitorSnapshot {
	tabs := m.monitorTabs()
	snapshots := make([]MonitorSnapshot, 0, len(tabs))
	for _, tab := range tabs {
		rendered := ""
		tab.mu.Lock()
		if tab.Terminal != nil {
			rendered = tab.Terminal.Render()
		}
		tab.mu.Unlock()
		snapshots = append(snapshots, MonitorSnapshot{
			Worktree:  tab.Worktree,
			Assistant: tab.Assistant,
			Name:      tab.Name,
			Running:   tab.Running,
			Rendered:  rendered,
		})
	}
	return snapshots
}

// MonitorTabs returns all tabs in a stable order for the monitor grid.
func (m *Model) MonitorTabs() []MonitorTab {
	tabs := m.monitorTabs()
	out := make([]MonitorTab, 0, len(tabs))
	for _, tab := range tabs {
		out = append(out, MonitorTab{
			ID:        tab.ID,
			Worktree:  tab.Worktree,
			Assistant: tab.Assistant,
			Name:      tab.Name,
			Running:   tab.Running,
		})
	}
	return out
}

// MonitorTabSnapshots returns monitor tabs with their visible screens.
func (m *Model) MonitorTabSnapshots() []MonitorTabSnapshot {
	return m.MonitorTabSnapshotsWithActive("")
}

// MonitorTabSnapshotsWithActive returns cached monitor tab snapshots.
// Snapshot generation is driven by monitor ticks to avoid render-time stalls.
func (m *Model) MonitorTabSnapshotsWithActive(activeID TabID) []MonitorTabSnapshot {
	m.monitorActiveID = activeID
	tabs := m.monitorTabs()
	snapshots := make([]MonitorTabSnapshot, 0, len(tabs))
	for _, tab := range tabs {
		if m.monitorSnapshotCache != nil {
			if snap, ok := m.monitorSnapshotCache[tab.ID]; ok {
				snapshots = append(snapshots, snap)
				continue
			}
		}
		snapshots = append(snapshots, MonitorTabSnapshot{
			MonitorTab: MonitorTab{
				ID:        tab.ID,
				Worktree:  tab.Worktree,
				Assistant: tab.Assistant,
				Name:      tab.Name,
				Running:   tab.Running,
			},
		})
	}
	return snapshots
}

type monitorSnapshotTick struct {
	full bool
}

type monitorSnapshotRequest struct {
	targets []monitorSnapshotTarget
}

type monitorSnapshotResult struct {
	snapshots map[TabID]MonitorTabSnapshot
}

type monitorSnapshotTarget struct {
	tab  *Tab
	meta MonitorTab
}

// StartMonitorSnapshots schedules the monitor snapshot loop.
func (m *Model) StartMonitorSnapshots() tea.Cmd {
	if !m.monitorMode {
		return nil
	}
	if m.msgSink == nil {
		return func() tea.Msg {
			return monitorSnapshotTick{full: true}
		}
	}
	if m.monitorSnapCh != nil {
		lastBeat := atomic.LoadInt64(&m.monitorSnapHeartbeat)
		if lastBeat > 0 && time.Since(time.Unix(0, lastBeat)) > 10*time.Second {
			m.StopMonitorSnapshots()
		}
	}
	if m.monitorSnapCh == nil {
		m.monitorSnapCh = make(chan monitorSnapshotRequest, 2)
		ctx, cancel := context.WithCancel(context.Background())
		m.monitorSnapCancel = cancel
		atomic.StoreInt64(&m.monitorSnapHeartbeat, time.Now().UnixNano())
		safego.Go("center.monitor_snapshots", func() {
			m.monitorSnapshotWorker(ctx)
		})
	}
	return func() tea.Msg {
		return monitorSnapshotTick{full: true}
	}
}

// RefreshMonitorSnapshots forces a full snapshot refresh (used by harness/tests).
func (m *Model) RefreshMonitorSnapshots() {
	tabs := m.monitorTabs()
	if len(tabs) == 0 {
		return
	}
	out := make(map[TabID]MonitorTabSnapshot, len(tabs))
	for _, tab := range tabs {
		if tab == nil {
			continue
		}
		if snap, ok := buildMonitorSnapshot(tab); ok {
			out[tab.ID] = snap
		}
	}
	m.applyMonitorSnapshotResult(out)
}

func (m *Model) handleMonitorSnapshotTick(msg monitorSnapshotTick) tea.Cmd {
	if !m.monitorMode {
		return nil
	}
	if m.monitorSnapCh != nil {
		lastBeat := atomic.LoadInt64(&m.monitorSnapHeartbeat)
		if lastBeat > 0 && time.Since(time.Unix(0, lastBeat)) > 10*time.Second {
			m.StopMonitorSnapshots()
			return m.StartMonitorSnapshots()
		}
	}
	if m.msgSink == nil {
		m.RefreshMonitorSnapshots()
	} else {
		m.enqueueMonitorSnapshots(msg.full)
	}
	interval := monitorSnapshotInterval(len(m.monitorTabs()))
	return common.SafeTick(interval, func(time.Time) tea.Msg {
		return monitorSnapshotTick{}
	})
}

func (m *Model) enqueueMonitorSnapshots(full bool) {
	tabs := m.monitorTabs()
	if m.monitorSnapshotCache == nil {
		m.monitorSnapshotCache = make(map[TabID]MonitorTabSnapshot, len(tabs))
	}
	if len(tabs) == 0 {
		m.monitorSnapshotCache = make(map[TabID]MonitorTabSnapshot)
		m.monitorSnapshotNext = 0
		return
	}

	activeSet := make(map[TabID]struct{}, len(tabs))
	for _, tab := range tabs {
		if tab != nil {
			activeSet[tab.ID] = struct{}{}
		}
	}
	for id := range m.monitorSnapshotCache {
		if _, ok := activeSet[id]; !ok {
			delete(m.monitorSnapshotCache, id)
		}
	}

	activeID := TabID("")
	if m.monitorActiveID != "" {
		if _, ok := activeSet[m.monitorActiveID]; ok {
			activeID = m.monitorActiveID
		}
	}
	if activeID == "" {
		selectedIdx := m.MonitorSelectedIndex(len(tabs))
		if selectedIdx >= 0 && selectedIdx < len(tabs) {
			activeID = tabs[selectedIdx].ID
		}
	}

	targets := m.collectMonitorSnapshotTargets(tabs, activeID, full)
	if len(targets) > 0 && m.monitorSnapCh != nil {
		select {
		case m.monitorSnapCh <- monitorSnapshotRequest{targets: targets}:
		default:
		}
	}
}

func (m *Model) collectMonitorSnapshotTargets(tabs []*Tab, activeID TabID, full bool) []monitorSnapshotTarget {
	if full {
		targets := make([]monitorSnapshotTarget, 0, len(tabs))
		for _, tab := range tabs {
			if tab == nil || tab.isClosed() {
				continue
			}
			targets = append(targets, newMonitorSnapshotTarget(tab))
		}
		return targets
	}
	var targets []monitorSnapshotTarget
	if activeID != "" {
		if activeTab := m.getTabByIDGlobal(activeID); activeTab != nil {
			if activeTab.isClosed() {
				return targets
			}
			targets = append(targets, newMonitorSnapshotTarget(activeTab))
		}
	}

	batch := monitorSnapshotBatchSize(len(tabs))
	if batch <= 0 {
		return targets
	}
	start := m.monitorSnapshotNext
	now := time.Now()
	for i := 0; i < batch; i++ {
		idx := (start + i) % len(tabs)
		tab := tabs[idx]
		if tab == nil || tab.isClosed() || tab.ID == activeID {
			continue
		}
		tab.mu.Lock()
		dirty := tab.monitorDirty
		stale := tab.monitorSnapAt.IsZero() || now.Sub(tab.monitorSnapAt) > 2*time.Second
		tab.mu.Unlock()
		if dirty || stale {
			targets = append(targets, newMonitorSnapshotTarget(tab))
		}
	}
	m.monitorSnapshotNext = (start + batch) % len(tabs)
	return targets
}

func (m *Model) monitorSnapshotWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-m.monitorSnapCh:
			atomic.StoreInt64(&m.monitorSnapHeartbeat, time.Now().UnixNano())
			if len(req.targets) == 0 {
				continue
			}
			out := make(map[TabID]MonitorTabSnapshot, len(req.targets))
			workers := monitorSnapshotWorkerCount(len(req.targets))
			if workers <= 1 {
				for _, target := range req.targets {
					if target.tab == nil {
						continue
					}
					if snap, ok := buildMonitorSnapshotWithMeta(target); ok {
						out[target.meta.ID] = snap
					}
				}
			} else {
				var mu sync.Mutex
				var wg sync.WaitGroup
				targets := make(chan monitorSnapshotTarget)
				for i := 0; i < workers; i++ {
					wg.Add(1)
					safego.Go("center.monitor_snapshot_worker", func() {
						defer wg.Done()
						for target := range targets {
							if target.tab == nil {
								continue
							}
							if snap, ok := buildMonitorSnapshotWithMeta(target); ok {
								mu.Lock()
								out[target.meta.ID] = snap
								mu.Unlock()
							}
						}
					})
				}
				for _, target := range req.targets {
					select {
					case targets <- target:
					case <-ctx.Done():
						close(targets)
						wg.Wait()
						return
					}
				}
				close(targets)
				wg.Wait()
			}
			if len(out) == 0 {
				continue
			}
			if m.msgSink != nil {
				m.msgSink(monitorSnapshotResult{snapshots: out})
			} else {
				m.applyMonitorSnapshotResult(out)
			}
		}
	}
}

// StopMonitorSnapshots terminates the snapshot worker.
func (m *Model) StopMonitorSnapshots() {
	if m.monitorSnapCancel != nil {
		m.monitorSnapCancel()
		m.monitorSnapCancel = nil
	}
	m.monitorSnapCh = nil
}

func buildMonitorSnapshot(tab *Tab) (MonitorTabSnapshot, bool) {
	snap := MonitorTabSnapshot{
		MonitorTab: MonitorTab{
			ID:        tab.ID,
			Worktree:  tab.Worktree,
			Assistant: tab.Assistant,
			Name:      tab.Name,
			Running:   tab.Running,
		},
	}
	return fillMonitorSnapshot(tab, snap), true
}

func newMonitorSnapshotTarget(tab *Tab) monitorSnapshotTarget {
	if tab == nil {
		return monitorSnapshotTarget{}
	}
	return monitorSnapshotTarget{
		tab: tab,
		meta: MonitorTab{
			ID:        tab.ID,
			Worktree:  tab.Worktree,
			Assistant: tab.Assistant,
			Name:      tab.Name,
			Running:   tab.Running,
		},
	}
}

func buildMonitorSnapshotWithMeta(target monitorSnapshotTarget) (MonitorTabSnapshot, bool) {
	snap := MonitorTabSnapshot{MonitorTab: target.meta}
	if target.tab == nil || target.tab.isClosed() {
		return snap, true
	}
	return fillMonitorSnapshot(target.tab, snap), true
}

func fillMonitorSnapshot(tab *Tab, snap MonitorTabSnapshot) MonitorTabSnapshot {
	if tab == nil || tab.isClosed() {
		return snap
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.Terminal == nil {
		return snap
	}
	version := tab.Terminal.Version()
	showCursor := false
	if tab.cachedSnap != nil &&
		tab.cachedVersion == version &&
		tab.cachedShowCursor == showCursor {
		applyMonitorSnapshot(&snap, tab.cachedSnap)
	} else {
		vsnap := compositor.NewVTermSnapshotWithCache(tab.Terminal, showCursor, tab.cachedSnap)
		if vsnap != nil {
			tab.cachedSnap = vsnap
			tab.cachedVersion = version
			tab.cachedShowCursor = showCursor
			applyMonitorSnapshot(&snap, vsnap)
		}
	}
	tab.monitorSnapAt = time.Now()
	tab.monitorDirty = false
	return snap
}

func (m *Model) applyMonitorSnapshotResult(snapshots map[TabID]MonitorTabSnapshot) {
	if m.monitorSnapshotCache == nil {
		m.monitorSnapshotCache = make(map[TabID]MonitorTabSnapshot)
	}
	for id, snap := range snapshots {
		m.monitorSnapshotCache[id] = snap
	}
}

func monitorSnapshotInterval(count int) time.Duration {
	switch {
	case count <= 8:
		return 33 * time.Millisecond
	case count <= 20:
		return 50 * time.Millisecond
	case count <= 40:
		return 66 * time.Millisecond
	default:
		return 80 * time.Millisecond
	}
}

func monitorSnapshotBatchSize(count int) int {
	switch {
	case count <= 8:
		return count
	case count <= 20:
		return 4
	case count <= 40:
		return 3
	default:
		return 2
	}
}

func monitorSnapshotWorkerCount(targets int) int {
	if targets <= 1 {
		return 1
	}
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	if workers > 4 {
		workers = 4
	}
	if workers > targets {
		workers = targets
	}
	return workers
}

func applyMonitorSnapshot(out *MonitorTabSnapshot, snap *compositor.VTermSnapshot) {
	if out == nil || snap == nil {
		return
	}
	out.Screen = snap.Screen
	out.CursorX = snap.CursorX
	out.CursorY = snap.CursorY
	out.ViewOffset = snap.ViewOffset
	out.Width = snap.Width
	out.Height = snap.Height
	out.SelActive = snap.SelActive
	out.SelStartX = snap.SelStartX
	out.SelStartY = snap.SelStartY
	out.SelEndX = snap.SelEndX
	out.SelEndY = snap.SelEndY
}

// ResizeTabs resizes the given tabs to the desired sizes.
func (m *Model) ResizeTabs(sizes []TabSize) {
	for _, size := range sizes {
		if size.Width < 1 || size.Height < 1 {
			continue
		}
		tab := m.getTabByIDGlobal(size.ID)
		if tab == nil || tab.isClosed() {
			continue
		}
		tab.mu.Lock()
		if tab.Terminal != nil {
			if tab.Terminal.Width != size.Width || tab.Terminal.Height != size.Height {
				tab.Terminal.Resize(size.Width, size.Height)
				tab.monitorDirty = true
			}
		}
		tab.mu.Unlock()
		m.resizePTY(tab, size.Height, size.Width)
	}
}

func (m *Model) monitorTabs() []*Tab {
	if m.monitorTabsCache != nil && m.monitorTabsRevision == m.tabsRevision {
		return m.monitorTabsCache
	}
	type monitorGroup struct {
		key  string
		tabs []*Tab
	}

	groups := make([]monitorGroup, 0, len(m.tabsByWorktree))
	for wtID, worktreeTabs := range m.tabsByWorktree {
		if len(worktreeTabs) == 0 {
			continue
		}
		key := wtID
		for _, tab := range worktreeTabs {
			if tab != nil && tab.Worktree != nil {
				key = tab.Worktree.Repo + "::" + tab.Worktree.Name
				break
			}
		}
		groups = append(groups, monitorGroup{key: key, tabs: worktreeTabs})
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].key < groups[j].key
	})

	var tabs []*Tab
	for _, group := range groups {
		for _, tab := range group.tabs {
			if tab != nil && !tab.isClosed() {
				tabs = append(tabs, tab)
			}
		}
	}

	m.monitorTabsCache = tabs
	m.monitorTabsRevision = m.tabsRevision
	return tabs
}

func (m *Model) getTabByIDGlobal(tabID TabID) *Tab {
	for wtID := range m.tabsByWorktree {
		if tab := m.getTabByID(wtID, tabID); tab != nil && !tab.isClosed() {
			return tab
		}
	}
	return nil
}

// MonitorSelectedIndex returns the clamped monitor selection.
func (m *Model) MonitorSelectedIndex(count int) int {
	return m.monitor.SelectedIndex(count)
}

// SetMonitorSelectedIndex updates the monitor selection.
func (m *Model) SetMonitorSelectedIndex(index, count int) {
	m.monitor.SetSelectedIndex(index, count)
}

// MoveMonitorSelection adjusts the monitor selection based on grid movement.
func (m *Model) MoveMonitorSelection(dx, dy, cols, rows, count int) {
	m.monitor.MoveSelection(dx, dy, cols, rows, count)
}

// ResetMonitorSelection clears monitor selection state.
func (m *Model) ResetMonitorSelection() {
	m.monitor.Reset()
}
