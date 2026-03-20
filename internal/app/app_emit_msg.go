package app

import tea "charm.land/bubbletea/v2"

func emitMsg(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}
