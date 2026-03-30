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
	defer tab.mu.Unlock()

	detached := tab.Detached
	reattachInFlight := tab.reattachInFlight
	isChat := m.isChatTabLocked(tab)

	if detached && !reattachInFlight && isChat {
		return true
	}
	if tab.DiffViewer != nil {
		return tab.DiffViewer.CanConsumeWheel()
	}
	if tab.Terminal != nil {
		return tab.Terminal.MaxViewOffset() > 0
	}
	return false
}
