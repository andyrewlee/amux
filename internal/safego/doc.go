// Package safego provides panic-safe goroutine helpers (Go, Run) with a
// pluggable PanicHandler, so background work can never crash the TUI on an
// otherwise-unrecovered panic.
package safego
