package app

import "github.com/andyrewlee/amux/internal/messages"

func (a *App) cycleAux(direction int) {
	modes := []AuxMode{AuxNone, AuxPreview, AuxDiff}
	idx := 0
	for i, mode := range modes {
		if mode == a.auxMode {
			idx = i
			break
		}
	}
	if direction > 0 {
		idx = (idx + 1) % len(modes)
	} else if direction < 0 {
		idx = (idx - 1 + len(modes)) % len(modes)
	}
	a.auxMode = modes[idx]
	if a.focusedPane == messages.PaneSidebar {
		a.focusPane(messages.PaneSidebar)
	}
}
