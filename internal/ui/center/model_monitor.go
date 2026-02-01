package center

import (
	"sort"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/vterm"
)

// MonitorSnapshot captures a tab display for the monitor grid.
type MonitorSnapshot struct {
	Workspace *data.Workspace
	Assistant string
	Name      string
	Running   bool
	Rendered  string
}

// MonitorTab describes a tab for the monitor grid.
type MonitorTab struct {
	ID        TabID
	Workspace *data.Workspace
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
	if tab.Workspace != nil {
		wtID = string(tab.Workspace.ID())
	}

	switch msg := msg.(type) {
	case tea.PasteMsg:
		// Handle bracketed paste - send entire content at once with escape sequences.
		if m.isTabActorReady() {
			if !m.sendTabEvent(tabEvent{
				tab:         tab,
				workspaceID: wtID,
				tabID:       tab.ID,
				kind:        tabEventPaste,
				pasteText:   msg.Content,
			}) {
				bracketedText := "\x1b[200~" + msg.Content + "\x1b[201~"
				if err := tab.Agent.Terminal.SendString(bracketedText); err != nil {
					logging.Warn("Monitor paste failed for tab %s: %v", tab.ID, err)
					tab.mu.Lock()
					tab.Running = false
					tab.Detached = true
					tab.mu.Unlock()
					return func() tea.Msg {
						return TabInputFailed{TabID: tab.ID, WorkspaceID: wtID, Err: err}
					}
				}
			}
		} else {
			bracketedText := "\x1b[200~" + msg.Content + "\x1b[201~"
			if err := tab.Agent.Terminal.SendString(bracketedText); err != nil {
				logging.Warn("Monitor paste failed for tab %s: %v", tab.ID, err)
				tab.mu.Lock()
				tab.Running = false
				tab.Detached = true
				tab.mu.Unlock()
				return func() tea.Msg {
					return TabInputFailed{TabID: tab.ID, WorkspaceID: wtID, Err: err}
				}
			}
		}
		return nil

	case tea.KeyPressMsg:
		switch {
		case msg.Key().Code == tea.KeyPgUp:
			if m.isTabActorReady() {
				if m.sendTabEvent(tabEvent{
					tab:         tab,
					workspaceID: wtID,
					tabID:       tab.ID,
					kind:        tabEventScrollPage,
					scrollPage:  1,
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
					tab:         tab,
					workspaceID: wtID,
					tabID:       tab.ID,
					kind:        tabEventScrollPage,
					scrollPage:  -1,
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
					tab:         tab,
					workspaceID: wtID,
					tabID:       tab.ID,
					kind:        tabEventScrollPage,
					scrollPage:  1,
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
					tab:         tab,
					workspaceID: wtID,
					tabID:       tab.ID,
					kind:        tabEventScrollPage,
					scrollPage:  -1,
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
					tab:         tab,
					workspaceID: wtID,
					tabID:       tab.ID,
					kind:        tabEventScrollToTop,
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
					tab:         tab,
					workspaceID: wtID,
					tabID:       tab.ID,
					kind:        tabEventScrollToBottom,
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
				tab:         tab,
				workspaceID: wtID,
				tabID:       tab.ID,
				kind:        tabEventScrollToBottom,
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
					tab:         tab,
					workspaceID: wtID,
					tabID:       tab.ID,
					kind:        tabEventSendInput,
					input:       input,
				}) {
					if err := tab.Agent.Terminal.SendString(string(input)); err != nil {
						logging.Warn("Monitor input failed for tab %s: %v", tab.ID, err)
						tab.mu.Lock()
						tab.Running = false
						tab.Detached = true
						tab.mu.Unlock()
						return func() tea.Msg {
							return TabInputFailed{TabID: tab.ID, WorkspaceID: wtID, Err: err}
						}
					}
				}
			} else {
				if err := tab.Agent.Terminal.SendString(string(input)); err != nil {
					logging.Warn("Monitor input failed for tab %s: %v", tab.ID, err)
					tab.mu.Lock()
					tab.Running = false
					tab.Detached = true
					tab.mu.Unlock()
					return func() tea.Msg {
						return TabInputFailed{TabID: tab.ID, WorkspaceID: wtID, Err: err}
					}
				}
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
			Workspace: tab.Workspace,
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
			Workspace: tab.Workspace,
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
				Workspace: tab.Workspace,
				Assistant: tab.Assistant,
				Name:      tab.Name,
				Running:   tab.Running,
			},
		})
	}
	return snapshots
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

	groups := make([]monitorGroup, 0, len(m.tabsByWorkspace))
	for wsID, workspaceTabs := range m.tabsByWorkspace {
		if len(workspaceTabs) == 0 {
			continue
		}
		key := wsID
		for _, tab := range workspaceTabs {
			if tab != nil && tab.Workspace != nil {
				key = tab.Workspace.Repo + "::" + tab.Workspace.Name
				break
			}
		}
		groups = append(groups, monitorGroup{key: key, tabs: workspaceTabs})
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
	for wtID := range m.tabsByWorkspace {
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
