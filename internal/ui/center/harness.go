package center

// AddTab appends a tab to the model. Used by harness/test code that builds tabs manually.
func (m *Model) AddTab(tab *Tab) {
	if m == nil || tab == nil || tab.Workspace == nil {
		return
	}
	if m.tabsByWorkspace == nil {
		m.tabsByWorkspace = make(map[string][]*Tab)
	}
	if m.activeTabByWorkspace == nil {
		m.activeTabByWorkspace = make(map[string]int)
	}
	wtID := string(tab.Workspace.ID())
	m.tabsByWorkspace[wtID] = append(m.tabsByWorkspace[wtID], tab)
	m.noteTabsChanged()
	if _, ok := m.activeTabByWorkspace[wtID]; !ok {
		m.activeTabByWorkspace[wtID] = 0
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
