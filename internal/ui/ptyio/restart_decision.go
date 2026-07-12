package ptyio

import "time"

// DecidePTYRestartLocked computes the shared restart-vs-detach decision after
// a PTY reader stops. It owns only the policy — panes keep their own side
// effects (messages, tab/terminal field writes) around the returned decision.
//
// termAlive reports whether the underlying terminal is still open. When it is,
// the restart budget is advanced via NextRestartBackoffLocked(window,
// maxRestarts): restart==true means the caller should schedule a restart after
// backoff; restart==false means the budget is exhausted and the caller should
// apply its detach side effects (RestartBackoff has already been zeroed by the
// budget check, matching the historical panes, while RestartCount/RestartSince
// keep the window). When termAlive is false the helper calls
// ResetRestartBackoffLocked — restarts no longer apply — and returns
// restart==false so the caller detaches.
//
// The caller must hold the state lock (same convention as
// NextRestartBackoffLocked), and should apply its detach side effects under
// that same critical section before releasing it.
func (st *State) DecidePTYRestartLocked(termAlive bool, window time.Duration, maxRestarts int) (restart bool, backoff time.Duration) {
	if !termAlive {
		st.ResetRestartBackoffLocked()
		return false, 0
	}
	backoff, ok := st.NextRestartBackoffLocked(window, maxRestarts)
	return ok, backoff
}
