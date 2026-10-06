//go:build windows

package data

import (
	"errors"

	"golang.org/x/sys/windows"
)

// transientOwnerAlive reports whether the recorded owner PID is still a live
// process. OpenProcess on a nonexistent PID fails ERROR_INVALID_PARAMETER —
// the one error that proves death; ACCESS_DENIED and anything else keep the
// hold conservatively. Tests stub this var to stage dead owners.
var transientOwnerAlive = func(pid int) bool {
	const stillActive = 259 // windows.STILL_ACTIVE
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return !errors.Is(err, windows.ERROR_INVALID_PARAMETER)
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return true // opened but exit-code probe failed — keep the hold
	}
	return code == stillActive
}
