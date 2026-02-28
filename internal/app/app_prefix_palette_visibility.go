package app

import "github.com/andyrewlee/amux/internal/messages"

func (a *App) prefixActionVisible(action string) bool {
	// Keep behavior permissive in lightweight tests that don't fully initialize App state.
	if a == nil || a.layout == nil || a.center == nil || a.sidebarTerminal == nil {
		return true
	}

	switch action {
	case "focus_left":
		return a.focusedPane != messages.PaneDashboard
	case "focus_right":
		switch a.focusedPane {
		case messages.PaneSidebar, messages.PaneSidebarTerminal:
			return false
		case messages.PaneCenter:
			return a.layout != nil && a.layout.ShowSidebar()
		default:
			return (a.layout != nil && a.layout.ShowCenter()) || (a.layout != nil && a.layout.ShowSidebar())
		}
	case "new_agent_tab", "new_terminal_tab":
		if a.activeWorkspace == nil || a.activeProject == nil {
			return false
		}
		return !a.tmuxCheckDone || a.tmuxAvailable
	case "delete_workspace":
		return a.activeWorkspace != nil && a.activeProject != nil
	case "next_tab", "prev_tab":
		switch a.focusedPane {
		case messages.PaneSidebarTerminal:
			return a.sidebarTerminal.HasMultipleTabs()
		case messages.PaneSidebar:
			return true
		default:
			return a.center.HasTabs()
		}
	case "close_tab", "detach_tab", "reattach_tab", "restart_tab":
		if a.focusedPane == messages.PaneSidebarTerminal {
			return true
		}
		return a.center.HasTabs()
	default:
		return true
	}
}

func (a *App) showNumericTabJump() bool {
	if a == nil || a.center == nil {
		return true
	}
	tabs, _ := a.center.GetTabsInfo()
	return len(tabs) > 1
}
