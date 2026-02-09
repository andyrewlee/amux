//go:build !windows

package process

import (
	"errors"
	"syscall"
)

func isTypedProcessGoneError(err error) bool {
	return errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.ECHILD)
}
