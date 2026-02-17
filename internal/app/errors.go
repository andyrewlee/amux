package app

import "github.com/andyrewlee/amux/internal/app/activity"

// errTmuxUnavailable is an alias for activity.ErrTmuxUnavailable, kept for
// backward compatibility with code outside the activity package (e.g., GC).
var errTmuxUnavailable = activity.ErrTmuxUnavailable
