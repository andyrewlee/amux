package app

import "github.com/andyrewlee/amux/internal/messages"

// syncPaneFocusFlags keeps child model focus flags consistent with focusedPane.
// This is a defensive invariant to prevent stale multi-cursor states.
func (a *App) syncPaneFocusFlags() {
	focusDashboard := func() {
		if a.dashboard != nil {
			a.dashboard.Focus()
		}
	}
	blurDashboard := func() {
		if a.dashboard != nil {
			a.dashboard.Blur()
		}
	}
	focusCenter := func() {
		if a.center != nil {
			a.center.Focus()
		}
	}
	blurCenter := func() {
		if a.center != nil {
			a.center.Blur()
		}
	}
	focusSidebar := func() {
		if a.sidebar != nil {
			a.sidebar.Focus()
		}
	}
	blurSidebar := func() {
		if a.sidebar != nil {
			a.sidebar.Blur()
		}
	}
	focusSidebarTerminal := func() {
		if a.sidebarTerminal != nil {
			a.sidebarTerminal.Focus()
		}
	}
	blurSidebarTerminal := func() {
		if a.sidebarTerminal != nil {
			a.sidebarTerminal.Blur()
		}
	}

	switch a.focusedPane {
	case messages.PaneDashboard:
		focusDashboard()
		blurCenter()
		blurSidebar()
		blurSidebarTerminal()
	case messages.PaneCenter:
		blurDashboard()
		focusCenter()
		blurSidebar()
		blurSidebarTerminal()
	case messages.PaneSidebar:
		blurDashboard()
		blurCenter()
		focusSidebar()
		blurSidebarTerminal()
	case messages.PaneSidebarTerminal:
		blurDashboard()
		blurCenter()
		blurSidebar()
		focusSidebarTerminal()
	}
}
