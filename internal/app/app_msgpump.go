package app

import (
	tea "charm.land/bubbletea/v2"
)

func (a *App) SetMsgSender(send func(tea.Msg)) {
	if send == nil {
		return
	}
	if a.externalMsgs == nil {
		a.externalMsgs = make(chan tea.Msg, 1024)
	}
	a.externalOnce.Do(func() {
		a.externalSender = send
		go a.drainExternalMsgs()
	})
}

func (a *App) enqueueExternalMsg(msg tea.Msg) {
	if msg == nil || a.externalMsgs == nil {
		return
	}
	a.externalMsgs <- msg
}

func (a *App) drainExternalMsgs() {
	for msg := range a.externalMsgs {
		if msg == nil || a.externalSender == nil {
			continue
		}
		a.externalSender(msg)
	}
}
