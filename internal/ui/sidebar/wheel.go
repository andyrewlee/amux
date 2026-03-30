package sidebar

// CanConsumeWheel reports whether the active sidebar tab has meaningful wheel
// scroll content. This avoids hover-wheel focus steals from empty panes.
func (m *TabbedSidebar) CanConsumeWheel() bool {
	if m == nil {
		return false
	}
	switch m.activeTab {
	case TabChanges:
		return m.changes.canConsumeWheel()
	case TabProject:
		return m.projectTree.canConsumeWheel()
	default:
		return false
	}
}

// CanConsumeWheel reports whether the active terminal has scrollback to view.
func (m *TerminalModel) CanConsumeWheel() bool {
	if m == nil {
		return false
	}
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx < 0 || activeIdx >= len(tabs) {
		return false
	}
	tab := tabs[activeIdx]
	if tab == nil || tab.State == nil {
		return false
	}
	tab.State.mu.Lock()
	defer tab.State.mu.Unlock()
	return tab.State.VTerm != nil && tab.State.VTerm.MaxViewOffset() > 0
}

func (m *Model) canConsumeWheel() bool {
	if m == nil || m.gitStatus == nil || m.gitStatus.Clean || len(m.displayItems) == 0 {
		return false
	}
	selectable := 0
	for _, item := range m.displayItems {
		if !item.isHeader {
			selectable++
		}
	}
	return selectable > 1
}

func (m *ProjectTree) canConsumeWheel() bool {
	if m == nil || m.workspace == nil || len(m.flatNodes) == 0 {
		return false
	}
	return len(m.flatNodes) > 1
}
