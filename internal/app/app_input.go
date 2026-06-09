package app

import (
	"fmt"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// Update handles all messages with panic recovery.
func (a *App) Update(msg tea.Msg) (model tea.Model, cmd tea.Cmd) {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("panic in app.Update: %v\n%s", r, debug.Stack())
			a.err = fmt.Errorf("internal error: %v", r)
			model = a
			cmd = nil
		}
	}()
	return a.update(msg)
}

func (a *App) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer perf.Time("update")()
	var cmds []tea.Cmd
	// Keep focus flags synchronized in Update (not View) so rendering remains
	// side-effect free while still enforcing single-pane cursor ownership.
	a.syncPaneFocusFlags()

	// Overlay/dialog input guards consume the message before the main routing.
	if res, consumed := a.handlePreSwitchInput(msg, &cmds); consumed {
		return a, res
	}

	if a.updateTabMsg(msg, &cmds) {
		return a, common.SafeBatch(cmds...)
	}
	if a.updateTmuxMsg(msg, &cmds) {
		return a, common.SafeBatch(cmds...)
	}
	if a.updateWorkspaceLifecycleMsg(msg, &cmds) {
		return a, common.SafeBatch(cmds...)
	}
	if a.updateDialogShowMsg(msg, &cmds) {
		return a, common.SafeBatch(cmds...)
	}
	if a.updateUpgradeMsg(msg, &cmds) {
		return a, common.SafeBatch(cmds...)
	}

	switch msg := msg.(type) {
	case tea.KeyboardEnhancementsMsg:
		a.handleKeyboardEnhancements(msg)

	case tea.WindowSizeMsg:
		a.handleWindowSize(msg)

	case tea.MouseClickMsg, tea.MouseWheelMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg:
		if cmd := a.handleMouseMsg(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.PasteMsg:
		if cmd := a.handlePaste(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case prefixTimeoutMsg:
		a.handlePrefixTimeout(msg)

	case tea.KeyPressMsg:
		if cmd := a.handleKeyPress(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case center.PTYOutput, center.PTYTick, center.PTYFlush, center.PTYStopped:
		if cmd := a.handlePTYMessages(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		// Sync active agents state to dashboard (show spinner only when actively outputting)
		a.syncActiveWorkspacesToDashboard()
		if startCmd := a.dashboard.StartSpinnerIfNeeded(); startCmd != nil {
			cmds = append(cmds, startCmd)
		}

	case messages.Toast:
		cmds = append(cmds, a.showToast(msg))

	case messages.SidebarPTYOutput, messages.SidebarPTYTick, messages.SidebarPTYFlush, messages.SidebarPTYStopped, messages.SidebarPTYRestart, sidebar.SidebarTerminalCreated, sidebar.SidebarTerminalCreateFailed, sidebar.SidebarTerminalReattachResult, sidebar.SidebarTerminalReattachFailed, sidebar.SidebarSelectionScrollTick:
		if cmd := a.handleSidebarPTYMessages(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case sidebar.OpenFileInEditor:
		if cmd := a.handleOpenFileInEditor(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.Error:
		if cmd := a.handleErrorMessage(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	default:
		// Forward unknown messages to center pane (e.g., commit viewer internal messages)
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return a, common.SafeBatch(cmds...)
}

// showToast routes a Toast message to the matching toast severity.
func (a *App) showToast(msg messages.Toast) tea.Cmd {
	switch msg.Level {
	case messages.ToastSuccess:
		return a.toast.ShowSuccess(msg.Message)
	case messages.ToastError:
		return a.toast.ShowError(msg.Message)
	case messages.ToastWarning:
		return a.toast.ShowWarning(msg.Message)
	default:
		return a.toast.ShowInfo(msg.Message)
	}
}

func (a *App) handleTabDetached(msg messages.TabDetached) tea.Cmd {
	if msg.WorkspaceID != "" {
		return a.persistWorkspaceTabs(msg.WorkspaceID)
	}
	return a.persistActiveWorkspaceTabs()
}
