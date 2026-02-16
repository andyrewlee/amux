package cli

import (
	"fmt"
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
		// Use Update to hold the workspace lock across Load+filter+Save,
		// preventing lost-update races between concurrent agent stop calls.
		err := svc.Store.Update(id, func(ws *data.Workspace) error {
			var tabs []data.TabInfo
			changed := false
			for _, tab := range ws.OpenTabs {
				if tab.SessionName == sessionName {
					changed = true
					continue
				}
				tabs = append(tabs, tab)
			}
			if !changed {
				return errNoChange
			}
			ws.OpenTabs = tabs
			return nil
		})
		if err == nil {
			return
		}
	}
}

// errNoChange is a sentinel used by removeTabFromStore to signal that the
// workspace did not contain the target tab and should not be re-saved.
var errNoChange = fmt.Errorf("no change")
