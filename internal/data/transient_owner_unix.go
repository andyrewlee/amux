//go:build !windows

package data

import (
	"errors"
	"os"
	"syscall"
)

// transientOwnerAlive reports whether the recorded owner PID is still a live
// process. Signal 0 probes existence without delivering: ESRCH means the
// PID is free (dead owner — the hold sweeps); EPERM means a foreign live
// process owns the PID (alive — the hold stays); any other error keeps the
// hold conservatively. Tests stub this var to stage dead owners
// deterministically.
var transientOwnerAlive = func(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
