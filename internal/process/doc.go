// Package process owns the lifecycle of OS processes around workspaces:
// cross-platform process-group teardown (KillProcessGroup) so agent process
// trees do not survive the tmux session that launched them, script running
// with port allocation (ScriptRunner), a durable registry of managed service
// groups so they stay stoppable across amux restarts (ServiceRegistry),
// process enumeration/attribution for workspaces (Snapshot, OrphanedGroups,
// StatsForWorkspace), and reaping of service stacks orphaned by deleted or
// prune-staged worktrees (FindWorkspaceOrphans).
package process
