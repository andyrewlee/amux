package cli

import (
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

var (
	tmuxSessionStateFor        = tmux.SessionStateFor
	tmuxKillSession            = tmux.KillSession
	tmuxSendKeys               = tmux.SendKeys
	tmuxSendInterrupt          = tmux.SendInterrupt
	tmuxSetSessionTag          = tmux.SetSessionTagValue
	tmuxStartSession           = tmuxNewSession
	startSendJobProcess        = launchSendJobProcessor
	appendWorkspaceOpenTabMeta = func(store *data.WorkspaceStore, wsID data.WorkspaceID, tab data.TabInfo) error {
		return store.AppendOpenTab(wsID, tab)
	}
)
