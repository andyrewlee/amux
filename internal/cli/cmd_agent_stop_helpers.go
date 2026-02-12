package cli

import (
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

func stopAgentSession(sessionName string, svc *Services, graceful bool, gracePeriod time.Duration) error {
	if !graceful {
		return tmuxKillSession(sessionName, svc.TmuxOpts)
	}
	if err := tmuxSendInterrupt(sessionName, svc.TmuxOpts); err != nil {
		return tmuxKillSession(sessionName, svc.TmuxOpts)
	}
	if gracePeriod <= 0 {
		return tmuxKillSession(sessionName, svc.TmuxOpts)
	}

	deadline := time.Now().Add(gracePeriod)
	for time.Now().Before(deadline) {
		state, err := tmuxSessionStateFor(sessionName, svc.TmuxOpts)
		if err == nil && !state.Exists {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return tmuxKillSession(sessionName, svc.TmuxOpts)
}

func removeTabFromStore(svc *Services, sessionName string) {
	ids, err := svc.Store.List()
	if err != nil {
		return
	}
	for _, id := range ids {
		ws, err := svc.Store.Load(id)
		if err != nil {
			continue
		}
		changed := false
		var tabs []data.TabInfo
		for _, tab := range ws.OpenTabs {
			if tab.SessionName == sessionName {
				changed = true
				continue
			}
			tabs = append(tabs, tab)
		}
		if changed {
			ws.OpenTabs = tabs
			_ = svc.Store.Save(ws)
			return
		}
	}
}
