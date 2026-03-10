package center

func (m *Model) assistantIsChat(assistant string) bool {
	if m != nil && m.config != nil && len(m.config.Assistants) > 0 {
		_, ok := m.config.Assistants[assistant]
		return ok
	}
	switch assistant {
	case "claude", "codex", "gemini", "amp", "opencode", "droid", "cline", "cursor", "pi":
		return true
	default:
		return false
	}
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
