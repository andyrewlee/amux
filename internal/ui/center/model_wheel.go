package center

// CanConsumeWheel reports whether the active center tab can meaningfully handle
// mouse-wheel input. Detached chat tabs count so wheel-driven focus can trigger
// reattach; otherwise a tab must have scrollable diff or terminal content.
func (m *Model) CanConsumeWheel() bool {
	if m == nil {
		return false
	}
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx < 0 || activeIdx >= len(tabs) {
		return false
	}
	tab := tabs[activeIdx]
	if tab == nil || tab.isClosed() {
		return false
	}

	tab.mu.Lock()
	detached := tab.Detached
	reattachInFlight := tab.reattachInFlight
	terminal := tab.Terminal
	diffViewer := tab.DiffViewer
	tab.mu.Unlock()

	if detached && !reattachInFlight && m.isChatTab(tab) {
		return true
	}
	if diffViewer != nil {
		return diffViewer.CanConsumeWheel()
	}
	if terminal != nil {
		return terminal.MaxViewOffset() > 0
	}
	return false
}
