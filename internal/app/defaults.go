package app

import "time"

const (
	// prefixTimeout controls how long prefix mode waits for a follow-up key.
	prefixTimeout = 700 * time.Millisecond

	// gitStatusTickInterval controls periodic git status refreshes.
	gitStatusTickInterval = 3 * time.Second

	// ptyWatchdogInterval controls how often we check PTY readers.
	ptyWatchdogInterval = 5 * time.Second

	// tmuxSyncDefaultInterval is the fallback interval for tmux session reconciliation.
	tmuxSyncDefaultInterval = 7 * time.Second

	// gitPathWaitInterval is the polling interval when waiting for a new worktree to expose .git.
	gitPathWaitInterval = 100 * time.Millisecond

	// persistDebounce controls workspace metadata save debouncing.
	persistDebounce = 500 * time.Millisecond

	// stateWatcherDebounce controls filesystem event coalescing for registry/workspace updates.
	stateWatcherDebounce = 200 * time.Millisecond

	// localWorkspaceReloadSuppressWindow suppresses watcher-driven workspace reloads
	// immediately after this process saves workspace metadata.
	localWorkspaceReloadSuppressWindow = 800 * time.Millisecond

	// tmuxActivityPrefilter controls the activity scan window for tmux sessions.
	tmuxActivityPrefilter = 120 * time.Second

	// tmuxActivityInterval controls how often we scan tmux sessions for activity.
	tmuxActivityInterval = 2 * time.Second

	// tmuxCommandTimeout caps tmux command duration for activity scans.
	tmuxCommandTimeout = 2 * time.Second

	// supervisorBackoff controls restart backoff for file/state watchers.
	supervisorBackoff = 500 * time.Millisecond

	// externalMsgBuffer is the size of the external message channel.
	externalMsgBuffer = 4096

	// externalCriticalBuffer is the size of the critical external message channel.
	externalCriticalBuffer = 512

	// defaultMaxAttachedAgentTabs limits concurrently attached chat PTYs to keep
	// UI responsiveness predictable under heavy multi-agent workloads.
	// AMUX_MAX_ATTACHED_AGENT_TABS=0 disables the limit.
	defaultMaxAttachedAgentTabs = 6
)
