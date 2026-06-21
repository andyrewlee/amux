package center

func (m *Model) assistantIsChat(assistant string) bool {
	if m == nil {
		return false
	}
	// Delegate to the single source of truth so activity detection and center
	// rendering classify chat agents identically, including the empty-config
	// path where it falls back to the canonical agent registry.
	return m.config.IsChatAssistant(assistant)
}

func tabHasDiffViewerLocked(tab *Tab) bool {
	return tab != nil && tab.DiffViewer != nil
}

func (m *Model) tabHasDiffViewer(tab *Tab) bool {
	if tab == nil {
		return false
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	return tabHasDiffViewerLocked(tab)
}

func (m *Model) isChatTabLocked(tab *Tab) bool {
	if tab == nil || tabHasDiffViewerLocked(tab) {
		return false
	}
	return m.assistantIsChat(tab.Assistant)
}

func (m *Model) isChatTab(tab *Tab) bool {
	if tab == nil {
		return false
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	return m.isChatTabLocked(tab)
}
