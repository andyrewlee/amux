package app

import (
	"context"
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/ui/center"
)

func (a *App) SetMsgSender(send func(tea.Msg)) {
	if send == nil {
		return
	}
	if a.externalMsgs == nil {
		a.externalMsgs = make(chan tea.Msg, 1024)
	}
	if a.externalCritical == nil {
		a.externalCritical = make(chan tea.Msg, 256)
	}
	a.externalOnce.Do(func() {
		a.externalSender = send
		safego.SetPanicHandler(func(name string, recovered any, _ []byte) {
			if a == nil {
				return
			}
			err := fmt.Errorf("background panic in %s: %v", name, recovered)
			a.enqueueExternalMsg(messages.Error{Err: err, Context: "background"})
		})
		a.installSupervisorErrorHandler()
		if a.supervisor != nil {
			a.supervisor.Start("app.external_msgs", a.runExternalMsgs)
			return
		}
		safego.Go("app.external_msgs", func() {
			_ = a.runExternalMsgs(context.Background())
		})
	})
}

func (a *App) enqueueExternalMsg(msg tea.Msg) {
	if msg == nil {
		return
	}
	if isCriticalExternalMsg(msg) {
		if a.externalCritical == nil {
			a.externalCritical = make(chan tea.Msg, 256)
		}
		select {
		case a.externalCritical <- msg:
			return
		default:
			if a.externalMsgs != nil {
				select {
				case <-a.externalMsgs:
					perf.Count("external_msg_drop_noncritical", 1)
				default:
				}
			}
			select {
			case a.externalCritical <- msg:
				return
			default:
				perf.Count("external_msg_drop_critical", 1)
				return
			}
		}
	}
	if a.externalMsgs == nil {
		a.externalMsgs = make(chan tea.Msg, 1024)
	}
	select {
	case a.externalMsgs <- msg:
	default:
		perf.Count("external_msg_drop", 1)
	}
}

func (a *App) runExternalMsgs(ctx context.Context) error {
	for {
		if a.externalCritical != nil {
			select {
			case msg := <-a.externalCritical:
				if msg != nil && a.externalSender != nil {
					a.externalSender(msg)
				}
				continue
			default:
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-a.externalCritical:
			if !ok {
				return nil
			}
			if msg == nil || a.externalSender == nil {
				continue
			}
			a.externalSender(msg)
		case msg, ok := <-a.externalMsgs:
			if !ok {
				return nil
			}
			if msg == nil || a.externalSender == nil {
				continue
			}
			a.externalSender(msg)
		}
	}
}

func (a *App) installSupervisorErrorHandler() {
	if a == nil || a.supervisor == nil {
		return
	}
	a.supervisor.SetErrorHandler(func(name string, err error) {
		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		a.enqueueExternalMsg(messages.Error{
			Err:     fmt.Errorf("worker %s: %w", name, err),
			Context: "worker",
		})
	})
}

func isCriticalExternalMsg(msg tea.Msg) bool {
	switch msg.(type) {
	case messages.Error, messages.SidebarPTYStopped, center.PTYStopped:
		return true
	default:
		return false
	}
}
