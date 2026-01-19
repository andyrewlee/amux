package center

import (
	"sort"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
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
	if tab == nil || tab.Agent == nil || tab.Agent.Terminal == nil {
		return nil
	}

	switch msg := msg.(type) {
	case tea.PasteMsg:
		// Handle bracketed paste - send entire content at once with escape sequences.
		bracketedText := "\x1b[200~" + msg.Content + "\x1b[201~"
		_ = tab.Agent.Terminal.SendString(bracketedText)
		return nil

	case tea.KeyPressMsg:
		switch {
		case msg.Key().Code == tea.KeyPgUp:
			tab.mu.Lock()
			if tab.Terminal != nil {
				tab.Terminal.ScrollView(tab.Terminal.Height / 4)
			}
			tab.mu.Unlock()
			return nil

		case msg.Key().Code == tea.KeyPgDown:
			tab.mu.Lock()
			if tab.Terminal != nil {
				tab.Terminal.ScrollView(-tab.Terminal.Height / 4)
			}
			tab.mu.Unlock()
			return nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+u"))):
			tab.mu.Lock()
			if tab.Terminal != nil {
				tab.Terminal.ScrollView(tab.Terminal.Height / 4)
			}
			tab.mu.Unlock()
			return nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+d"))):
			tab.mu.Lock()
			if tab.Terminal != nil {
				tab.Terminal.ScrollView(-tab.Terminal.Height / 4)
			}
			tab.mu.Unlock()
			return nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("home"))):
			tab.mu.Lock()
			if tab.Terminal != nil {
				tab.Terminal.ScrollViewToTop()
			}
			tab.mu.Unlock()
			return nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("end"))):
			tab.mu.Lock()
			if tab.Terminal != nil {
				tab.Terminal.ScrollViewToBottom()
			}
			tab.mu.Unlock()
			return nil
		}

		// If scrolled, any typing goes back to live and sends key.
		tab.mu.Lock()
		if tab.Terminal != nil && tab.Terminal.IsScrolled() {
			tab.Terminal.ScrollViewToBottom()
		}
		tab.mu.Unlock()

		input := common.KeyToBytes(msg)
		if len(input) > 0 {
			_ = tab.Agent.Terminal.SendString(string(input))
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
	tabs := m.monitorTabs()
	snapshots := make([]MonitorTabSnapshot, 0, len(tabs))
	for _, tab := range tabs {
		snap := MonitorTabSnapshot{
			MonitorTab: MonitorTab{
				ID:        tab.ID,
				Worktree:  tab.Worktree,
				Assistant: tab.Assistant,
				Name:      tab.Name,
				Running:   tab.Running,
			},
		}
		tab.mu.Lock()
		if tab.Terminal != nil {
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
		}
		tab.mu.Unlock()
		snapshots = append(snapshots, snap)
	}
	return snapshots
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
		if tab == nil {
			continue
		}
		tab.mu.Lock()
		if tab.Terminal != nil {
			if tab.Terminal.Width != size.Width || tab.Terminal.Height != size.Height {
				tab.Terminal.Resize(size.Width, size.Height)
			}
		}
		tab.mu.Unlock()
		m.resizePTY(tab, size.Height, size.Width)
	}
}

func (m *Model) monitorTabs() []*Tab {
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
			if tab != nil {
				tabs = append(tabs, tab)
			}
		}
	}

	return tabs
}

func (m *Model) getTabByIDGlobal(tabID TabID) *Tab {
	for wtID := range m.tabsByWorktree {
		if tab := m.getTabByID(wtID, tabID); tab != nil {
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
