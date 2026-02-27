package center

// applyTerminalCursorPolicyLocked applies per-tab cursor policy to the vterm.
// Caller must hold tab.mu.
func (m *Model) applyTerminalCursorPolicyLocked(tab *Tab) {
	if m == nil || tab == nil || tab.Terminal == nil {
		return
	}
	isChat := m.isChatTab(tab)
	tab.Terminal.IgnoreCursorVisibilityControls = isChat
}
