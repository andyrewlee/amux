package app

import "github.com/andyrewlee/amux/internal/messages"

func (a *App) prefixActionVisible(action prefixAction) bool {
	// Keep behavior permissive in lightweight tests that don't fully initialize App state.
	if a == nil || a.layout == nil || a.center == nil || a.sidebarTerminal == nil {
		return true
	}

	switch action {
	case prefixActionFocusLeft:
		return a.focusedPane != messages.PaneDashboard
	case prefixActionFocusRight:
		switch a.focusedPane {
		case messages.PaneSidebar, messages.PaneSidebarTerminal:
			return false
		case messages.PaneCenter:
			return a.layout != nil && a.layout.ShowSidebar()
		default:
			return (a.layout != nil && a.layout.ShowCenter()) || (a.layout != nil && a.layout.ShowSidebar())
		}
	case prefixActionNewAgentTab, prefixActionNewTerminalTab:
		if a.activeWorkspace == nil || a.activeProject == nil {
			return false
		}
		return !a.tmuxCheckDone || a.tmuxAvailable
	case prefixActionScrollUp, prefixActionScrollDown:
		return a.centerScrollPrefixActive()
	case prefixActionDeleteWorkspace:
		return a.activeWorkspace != nil && a.activeProject != nil
	case prefixActionBrowseScripts:
		// The transcript browser opens the file in a workspace-hosted viewer
		// tab — hide it when there's no workspace to host on.
		return a.activeWorkspace != nil

	case prefixActionNextTab, prefixActionPrevTab:
		switch a.focusedPane {
		case messages.PaneSidebarTerminal:
			return a.sidebarTerminal.HasMultipleTabs()
		case messages.PaneSidebar:
			return true
		default:
			return a.center.HasTabs()
		}
	case prefixActionCloseTab, prefixActionDetachTab, prefixActionReattachTab, prefixActionRestartTab:
		if a.focusedPane == messages.PaneSidebarTerminal {
			return true
		}
		return a.center.HasTabs()
	case prefixActionCopyTranscript, prefixActionSaveTranscript:
		switch a.focusedPane {
		case messages.PaneSidebarTerminal:
			return a.sidebarTerminal.HasActiveTerminal()
		case messages.PaneCenter:
			return a.center.HasActiveTerminal()
		default:
			return false
		}
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
