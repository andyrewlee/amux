package ptyio

import "time"

// ReattachGuard is the lock+stamp state behind every reattach-stall sweep
// (center agent tabs and the sidebar terminal share it). The caller holds the
// enclosing struct's mutex across Begin/Sweep and performs the release
// action — clearing Running, emitting toasts — itself: the guard owns only the
// in-flight flag and the acquisition stamp, the subtle part both panes used to
// implement separately.
type ReattachGuard struct {
	// InFlight prevents overlapping reattach attempts for the same tab.
	InFlight bool
	// StartedAt is when InFlight was last acquired; the sweep uses it to tell
	// a slow reattach from one whose outcome was dropped, misrouted, or never
	// produced (which would otherwise pin the tab forever — every retry
	// no-ops behind the same lock).
	StartedAt time.Time
}

// Begin acquires the lock and stamps the acquisition.
func (g *ReattachGuard) Begin() {
	g.InFlight = true
	g.StartedAt = time.Now()
}

// Sweep applies the stall decision for one guarded item. It reports whether
// the caller should run its release action: a running item holds no
// meaningful lock even if the flag lingers, and a zero stamp means the flag
// was set outside Begin — stamp it now rather than releasing something never
// timed. Only an in-flight lock older than ReattachStallTimeout is released.
func (g *ReattachGuard) Sweep(now time.Time, running bool) (released bool) {
	switch {
	case !g.InFlight || running:
	case g.StartedAt.IsZero():
		g.StartedAt = now
	case now.Sub(g.StartedAt) > ReattachStallTimeout:
		g.InFlight = false
		released = true
	}
	return released
}
