//go:build windows

package process

import "errors"

// Snapshot is not implemented on Windows: there is no pgid-based service
// lifecycle there (see treekill_windows.go), so teardown and reaping degrade
// to the tmux-session and script-runner paths only.
func Snapshot() ([]ProcessInfo, error) {
	return nil, errors.ErrUnsupported
}
