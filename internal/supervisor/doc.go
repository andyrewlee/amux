// Package supervisor runs named background workers under a restart/backoff
// policy and surfaces fatal worker errors through a pluggable error handler
// instead of letting an unrecovered failure take down the app.
package supervisor
