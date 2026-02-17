package activity

import "errors"

// ErrTmuxUnavailable is returned when the tmux service is nil or missing.
var ErrTmuxUnavailable = errors.New("tmux service unavailable")
