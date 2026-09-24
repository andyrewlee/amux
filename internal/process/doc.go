// Package process manages external processes: cross-platform process-group
// teardown (KillProcessGroup) so agent trees do not survive the tmux session
// that launched them; the workspace script subsystem (ScriptRunner — config
// loading, trust gating, setup/run/archive/on-done lifecycle, captured
// output); run sessions; and per-workspace port allocation.
package process
