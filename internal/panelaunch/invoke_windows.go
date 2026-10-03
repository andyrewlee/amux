//go:build windows

package panelaunch

// Compile-only stubs: pane launch consumes payloads via syscall.Exec, which
// has no Windows equivalent in this codebase.

func runPayload(string) int { return exitFailure }

func discardPayload(string) int { return exitFailure }
