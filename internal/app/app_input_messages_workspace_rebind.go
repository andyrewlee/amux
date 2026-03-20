package app

import tea "charm.land/bubbletea/v2"

func (a *App) rebindActiveSelection() []tea.Cmd {
	cmds, _ := a.rebindActiveSelectionWithRecoverySkips()
	return cmds
}
