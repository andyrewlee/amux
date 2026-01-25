package center

// AddTab appends a tab to the model. Used by harness/test code that builds tabs manually.
func (m *Model) AddTab(tab *Tab) {
	if m == nil || tab == nil || tab.Worktree == nil {
		return
	}
	if m.tabsByWorktree == nil {
		m.tabsByWorktree = make(map[string][]*Tab)
	}
	if m.activeTabByWorktree == nil {
		m.activeTabByWorktree = make(map[string]int)
	}
	wtID := string(tab.Worktree.ID())
	m.tabsByWorktree[wtID] = append(m.tabsByWorktree[wtID], tab)
	m.noteTabsChanged()
	if _, ok := m.activeTabByWorktree[wtID]; !ok {
		m.activeTabByWorktree[wtID] = 0
	}
}

// WriteToTerminal writes bytes to the tab terminal while holding the tab lock.
func (t *Tab) WriteToTerminal(data []byte) {
	if t == nil || t.Terminal == nil {
		return
	}
	t.mu.Lock()
	t.Terminal.Write(data)
	t.mu.Unlock()
}
