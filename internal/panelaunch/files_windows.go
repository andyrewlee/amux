//go:build windows

package panelaunch

import (
	"errors"
	"os"
)

// Windows is compile-only: amux requires tmux and never launches panes
// there. Every filesystem-ownership operation reports unsupported so the
// shared prepare/cleanup code compiles without making a runtime promise.

var errUnsupported = errors.New("pane launch: unsupported on this platform")

func euidOwns(os.FileInfo) bool { return false }

func checkAttemptDir(string) error { return errUnsupported }

func openPayload(string) (*os.File, error) { return nil, errUnsupported }

func removeOwnedPayload(string) error { return errUnsupported }
