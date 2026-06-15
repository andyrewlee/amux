package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// supervisorErrorToastInterval throttles repeated worker-error toasts so a
// worker failing on a tight restart loop does not emit a toast every cycle. The
// first error for a worker always notifies; subsequent ones are suppressed
// until this interval elapses.
const supervisorErrorToastInterval = 30 * time.Second

func (a *App) SetMsgSender(send func(tea.Msg)) {
	if send == nil {
		return
	}
	a.externalOnce.Do(func() {
		a.externalSender = send
		safego.SetPanicHandler(func(name string, recovered any, _ []byte) {
			if a == nil {
				return
			}
			err := fmt.Errorf("background panic in %s: %v", name, recovered)
			a.enqueueExternalMsg(messages.Error{Err: err, Context: errorContext(errorServiceApp, "background")})
		})
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
	_ = a.tryEnqueueExternalMsg(msg)
}

func (a *App) tryEnqueueExternalMsg(msg tea.Msg) bool {
	if msg == nil {
		return false
	}
	if isCriticalExternalMsg(msg) {
		// Critical messages are non-evicting: if the critical queue is full they
		// drop themselves rather than evicting a queued non-critical message.
		select {
		case a.externalCritical <- msg:
			return true
		default:
			perf.Count("external_msg_drop_critical", 1)
			return false
		}
	}
	select {
	case a.externalMsgs <- msg:
		return true
	default:
		perf.Count("external_msg_drop", 1)
		return false
	}
}

func (a *App) runExternalMsgs(ctx context.Context) error {
	for {
		// Fast-path: drain critical messages first (non-blocking)
		select {
		case msg, ok := <-a.externalCritical:
			if !ok {
				return nil
			}
			if msg != nil && a.externalSender != nil {
				a.externalSender(msg)
			} else if msg != nil {
				logging.Warn("critical message dropped: sender not initialized")
			}
			continue
		default:
		}
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-a.externalCritical:
			if !ok {
				return nil
			}
			if msg == nil {
				continue
			}
			if a.externalSender == nil {
				logging.Warn("critical message dropped: sender not initialized")
				continue
			}
			a.externalSender(msg)
		case msg, ok := <-a.externalMsgs:
			if !ok {
				return nil
			}
			if msg == nil {
				continue
			}
			if a.externalSender == nil {
				logging.Warn("message dropped: sender not initialized")
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
	var (
		mu           sync.Mutex
		lastNotified = make(map[string]time.Time)
	)
	a.supervisor.SetErrorHandler(func(name string, err error) {
		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		now := time.Now()
		mu.Lock()
		last, seen := lastNotified[name]
		if seen && now.Sub(last) < supervisorErrorToastInterval {
			mu.Unlock()
			return // throttled: this worker toasted too recently
		}
		lastNotified[name] = now
		mu.Unlock()
		a.enqueueExternalMsg(messages.Error{
			Err:     fmt.Errorf("worker %s: %w", name, err),
			Context: errorContext(errorServiceSupervisor, "worker"),
		})
	})
}

func isCriticalExternalMsg(msg tea.Msg) bool {
	if _, ok := msg.(common.CriticalExternalMsg); ok {
		return true
	}
	switch msg.(type) {
	case messages.Error, messages.SidebarPTYStopped, center.PTYStopped:
		return true
	default:
		return false
	}
}
