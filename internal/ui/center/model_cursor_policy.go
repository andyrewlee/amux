package center

// applyTerminalCursorPolicyLocked applies per-tab cursor policy to the vterm.
// Caller must hold tab.mu.
func (m *Model) applyTerminalCursorPolicyLocked(tab *Tab) {
	if m == nil || tab == nil || tab.Terminal == nil {
		return
	}
	isChat := m.isChatTabLocked(tab)
	// Chat tabs must observe DECTCEM hide/show so app-owned cursors (for example
	// Ink/Claude-style painted cursors) can suppress the delegated amux cursor.
	// Non-chat tabs should also honor the terminal app's cursor visibility
	// directly, so this stays false for every tab.
	tab.Terminal.IgnoreCursorVisibilityControls = false
	tab.Terminal.TreatLFAsCRLF = isChat
}
