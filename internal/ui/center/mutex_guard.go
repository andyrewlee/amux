package center

import (
	"sync"

	"github.com/andyrewlee/amux/internal/syncutil"
)

// withMu runs fn under mu and releases it even when fn panics — a panic
// must not leave the pane mutex locked, or the supervised actor restart
// (and the update loop) deadlocks on it and the whole UI freezes. The
// mechanics live in syncutil; this keeps the pane-local spelling.
//
// New critical sections on Tab.mu must use this (or an explicit
// `defer mu.Unlock()` when the lock is taken at function top). Never take
// tab.mu across a call that can panic without a deferred unlock.
func withMu(mu *sync.Mutex, fn func()) {
	syncutil.WithMu(mu, fn)
}

// withMuVal is withMu for sections that produce a value.
func withMuVal[T any](mu *sync.Mutex, fn func() T) T {
	return syncutil.WithMuVal(mu, fn)
}
