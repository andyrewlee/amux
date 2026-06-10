package center

// AddTab appends a tab to the model. Used by harness/test code that builds tabs manually.
func (m *Model) AddTab(tab *Tab) {
	if m == nil || tab == nil || tab.Workspace == nil {
		return
	}
	if m.tabs.ByWorkspace == nil {
		m.tabs.ByWorkspace = make(map[string][]*Tab)
	}
	if m.tabs.ActiveByWorkspace == nil {
		m.tabs.ActiveByWorkspace = make(map[string]int)
	}
	wtID := string(tab.Workspace.ID())
	if tab.Terminal != nil {
		tab.Terminal.IgnoreCursorVisibilityControls = false
	}
	m.tabs.ByWorkspace[wtID] = append(m.tabs.ByWorkspace[wtID], tab)
	m.noteTabsChanged()
	if _, ok := m.tabs.ActiveByWorkspace[wtID]; !ok {
		m.tabs.ActiveByWorkspace[wtID] = 0
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
