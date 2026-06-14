package center

import "github.com/andyrewlee/amux/internal/config"

func (m *Model) assistantIsChat(assistant string) bool {
	if m != nil && m.config != nil && len(m.config.Assistants) > 0 {
		_, ok := m.config.Assistants[assistant]
		return ok
	}
	// Fallback when no config is loaded: consult the canonical agent registry
	// so the supported roster stays in lockstep with config/theme.
	return config.IsRegisteredAgent(assistant)
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
