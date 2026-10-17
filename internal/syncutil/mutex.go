// Package syncutil holds tiny shared synchronization helpers used by the
// pane packages — leaf dependency, no amux imports.
package syncutil

import "sync"

// WithMu runs fn under mu and releases it even when fn panics — a panic
// must not leave a pane mutex locked, or the supervised actor restart
// (and the update loop) deadlocks on it and the whole UI freezes.
//
// Critical sections on a pane's state mutex must use this (or an explicit
// `defer mu.Unlock()` when the lock is taken at function top). Never take
// a pane mutex across a call that can panic without a deferred unlock.
func WithMu(mu *sync.Mutex, fn func()) {
	mu.Lock()
	defer mu.Unlock()
	fn()
}

// WithMuVal is WithMu for sections that produce a value.
func WithMuVal[T any](mu *sync.Mutex, fn func() T) T {
	mu.Lock()
	defer mu.Unlock()
	return fn()
}
