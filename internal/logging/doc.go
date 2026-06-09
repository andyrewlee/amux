// Package logging is amux's file-based logger. Internal packages route
// diagnostics through it (Debug/Info/Warn/Error) instead of writing to
// stdout/stderr, which is reserved for the CLI entrypoints.
//
// The minimum level defaults to INFO; set AMUX_LOG_LEVEL=debug (or info/warn/
// error) to change it, which is required to surface the Debug call sites.
// AMUX_LOG_RETENTION_DAYS controls how many days of log files are retained.
package logging
