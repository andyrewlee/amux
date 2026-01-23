package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
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
	select {
	case a.externalMsgs <- msg:
	default:
		a.logExternalDrop()
	}
}

func (a *App) drainExternalMsgs() {
	for msg := range a.externalMsgs {
		if msg == nil || a.externalSender == nil {
			continue
		}
		a.externalSender(msg)
	}
}

func (a *App) logExternalDrop() {
	now := time.Now().UnixNano()
	last := a.externalDropLastLog.Load()
	if now-last < int64(time.Second) {
		return
	}
	if !a.externalDropLastLog.CompareAndSwap(last, now) {
		return
	}
	logging.Warn("External message queue full; dropping PTY messages")
}
