// Package process provides cross-platform process-group teardown: it kills a
// process together with its descendants (KillProcessGroup) so agent process
// trees do not survive the tmux session that launched them.
package process
