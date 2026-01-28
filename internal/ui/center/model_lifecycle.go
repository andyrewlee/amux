package center

// SetSize sets the center pane size
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Use centralized metrics for terminal sizing
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height

	// CommitViewer uses the same dimensions
	viewerWidth := termWidth
	viewerHeight := termHeight

	// Update all terminals across all workspaces
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			tab.mu.Lock()
			if tab.Terminal != nil {
				if tab.Terminal.Width != termWidth || tab.Terminal.Height != termHeight {
					tab.Terminal.Resize(termWidth, termHeight)
				}
			}
			if tab.DiffViewer != nil {
				tab.DiffViewer.SetSize(viewerWidth, viewerHeight)
			}
			tab.mu.Unlock()
			m.resizePTY(tab, termHeight, termWidth)
		}
	}
}

// SetOffset sets the X offset of the pane from screen left (for mouse coordinate conversion)
func (m *Model) SetOffset(x int) {
	m.offsetX = x
}

// Close cleans up all resources
func (m *Model) Close() {
	m.StopMonitorSnapshots()
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			tab.markClosing()
			m.stopPTYReader(tab)
			tab.mu.Lock()
			if tab.ptyTraceFile != nil {
				_ = tab.ptyTraceFile.Close()
				tab.ptyTraceFile = nil
				tab.ptyTraceClosed = true
			}
			tab.pendingOutput = nil
			tab.DiffViewer = nil
			tab.Running = false
			tab.mu.Unlock()
			tab.markClosed()
		}
	}
	if m.agentManager != nil {
		m.agentManager.CloseAll()
	}
}

// TickSpinner advances the spinner animation frame.
func (m *Model) TickSpinner() {
	m.spinnerFrame++
}

// screenToTerminal converts screen coordinates to terminal coordinates
// Returns the terminal X, Y and whether the coordinates are within the terminal content area
func (m *Model) screenToTerminal(screenX, screenY int) (termX, termY int, inBounds bool) {
	// Use centralized metrics for consistent geometry
	tm := m.terminalMetrics()

	// X offset includes pane position + border + padding
	contentStartX := m.offsetX + tm.ContentStartX
	// Y offset is just border + tab bar (pane Y starts at 0)
	contentStartY := tm.ContentStartY

	// Convert screen coordinates to terminal coordinates
	termX = screenX - contentStartX
	termY = screenY - contentStartY

	// Check bounds
	inBounds = termX >= 0 && termX < tm.Width && termY >= 0 && termY < tm.Height
	return
}
