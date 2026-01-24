package safego

import (
	"runtime/debug"
	"sync"

	"github.com/andyrewlee/amux/internal/logging"
)

// PanicHandler receives panic details from recovered goroutines.
type PanicHandler func(name string, recovered any, stack []byte)

var (
	panicHandlerMu sync.RWMutex
	panicHandler   PanicHandler
)

// SetPanicHandler registers a global handler for recovered panics.
func SetPanicHandler(handler PanicHandler) {
	panicHandlerMu.Lock()
	panicHandler = handler
	panicHandlerMu.Unlock()
}

// Run executes fn and converts panics into logged errors.
// This does not recover from runtime-fatal errors (e.g., concurrent map writes).
func Run(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			label := name
			if label == "" {
				label = "goroutine"
			}
			stack := debug.Stack()
			logging.Error("panic in %s: %v\n%s", label, r, stack)
			panicHandlerMu.RLock()
			handler := panicHandler
			panicHandlerMu.RUnlock()
			if handler != nil {
				func() {
					defer func() { _ = recover() }()
					handler(label, r, stack)
				}()
			}
		}
	}()
	fn()
}

// Go runs fn in a new goroutine with panic recovery.
func Go(name string, fn func()) {
	go Run(name, fn)
}
