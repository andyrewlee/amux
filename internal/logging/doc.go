// Package logging is amux's file-based logger. Internal packages route
// diagnostics through it (Debug/Info/Warn/Error) instead of writing to
// stdout/stderr, which is reserved for the CLI entrypoints.
//
// The minimum level defaults to INFO; set AMUX_LOG_LEVEL=debug (or info/warn/
// error) to change it, which is required to surface the Debug call sites.
// AMUX_LOG_RETENTION_DAYS controls how many days of log files are retained.
//
// Error vs Warn is a contract, not a severity knob: Error = a user-initiated
// operation failed and the app continued (a shelve/kill/persist/send the user
// asked for did not happen); Warn = a background path is degraded, retrying,
// or self-healing (GC sweeps, discovery retries, fallbacks, rejected input
// that stays internal). AMUX_LOG_LEVEL=error must surface exactly the
// failures a user needs to see — so a user-visible failure must log Error
// even when it also shows a toast or dialog.
package logging
