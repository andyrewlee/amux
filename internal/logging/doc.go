// Package logging is amux's file-based logger. Internal packages route
// diagnostics through it (Debug/Info/Warn/Error) instead of writing to
// stdout/stderr, which is reserved for the CLI entrypoints.
package logging
