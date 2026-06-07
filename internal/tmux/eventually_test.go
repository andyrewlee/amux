package tmux

import "time"

// eventually polls cond until it returns true or timeout elapses, then reports
// whether it ever held. It replaces fixed settle-sleeps, which flake on a loaded
// or slow host and give a real regression a fixed window to hide inside.
func eventually(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}
