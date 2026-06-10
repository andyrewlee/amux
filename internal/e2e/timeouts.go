package e2e

import "time"

// Timeouts and poll intervals shared across the e2e suite. Each constant
// documents the real-world event it bounds so individual tests don't grow
// their own slightly-different numbers.
const (
	// closeLoopTimeout bounds one full close-the-loop pass: app start, UI
	// render, fake-agent spawn inside tmux, and the keystroke bytes landing in
	// the agent's log file.
	closeLoopTimeout = 30 * time.Second

	// workspaceAgentTimeout bounds workspace creation (git worktree add) plus
	// agent tab spawn and the matching tmux session appearing.
	workspaceAgentTimeout = 30 * time.Second

	// persistenceTimeout bounds an app start/quit cycle around persisted tmux
	// sessions; shorter than the agent timeouts because no worktree is created.
	persistenceTimeout = 15 * time.Second

	// prefixInterKeyDelay spaces the keys of a leader-key sequence far enough
	// apart for the app's prefix-mode state machine to observe each key.
	prefixInterKeyDelay = 15 * time.Millisecond

	// sessionPollInterval paces tmux list-sessions polling; tmux session
	// state changes at human speed, so 200ms keeps subprocess churn low.
	sessionPollInterval = 200 * time.Millisecond

	// screenPollInterval paces rendered-screen polling for helpers that watch
	// the PTY screen without an update signal (e.g. never-contains checks).
	screenPollInterval = 150 * time.Millisecond

	// condPollInterval paces in-process condition polling (file contents,
	// in-memory flags) where checks are cheap.
	condPollInterval = 10 * time.Millisecond
)
