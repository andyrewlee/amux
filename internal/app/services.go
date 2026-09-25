package app

import (
	"time"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/update"
)

// The workspace-service interfaces (ProjectRegistry, WorkspaceStore,
// GitOperations) live in internal/app/workspacesvc — the only package that
// consumes them.

// GitStatusService provides cached status reads and fresh refreshes.
type GitStatusService interface {
	GetCached(root string) *git.StatusResult
	UpdateCache(root string, status *git.StatusResult)
	Invalidate(root string)
	Refresh(root string) (*git.StatusResult, error)
	RefreshFast(root string) (*git.StatusResult, error)
}

// TmuxOps defines tmux operations used by the app.
type TmuxOps interface {
	EnsureAvailable() error
	InstallHint() string
	ActiveAgentSessionsByActivity(window time.Duration, opts tmux.Options) ([]tmux.SessionActivity, error)
	SessionsWithTags(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error)
	// SetSessionTagValueForSessions is declared on the interface (not asserted)
	// because owner-heartbeat refresh has no fallback: a TmuxOps lacking it
	// silently breaks the cross-instance liveness the GC relies on.
	SetSessionTagValueForSessions(sessionNames []string, key, value string, opts tmux.Options) error
	AllSessionStates(opts tmux.Options) (map[string]tmux.SessionState, error)
	// AllSessionMeta returns attached-client count and creation time for every
	// session in one call, so scan loops can skip per-session probes.
	AllSessionMeta(opts tmux.Options) (map[string]tmux.SessionMeta, error)
	KillSession(sessionName string, opts tmux.Options) error
	KillSessionsMatchingTags(tags map[string]string, opts tmux.Options) (bool, error)
	KillSessionsWithPrefix(prefix string, opts tmux.Options) error
	KillSessionsWithPrefixMissingTag(prefix, tag string, opts tmux.Options) error
	KillWorkspaceSessions(wsID string, opts tmux.Options) error
	SetMonitorActivityOn(opts tmux.Options) error
	SetStatusOff(opts tmux.Options) error
	CapturePaneTail(sessionName string, lines int, opts tmux.Options) (string, bool)
	// CapturePaneTailChecked is CapturePaneTail with the active-pane liveness
	// probe supplied by the caller (e.g. from AllSessionStates), saving one
	// tmux subprocess per call.
	CapturePaneTailChecked(sessionName string, lines int, activePaneLive bool, opts tmux.Options) (string, bool)
	ContentHash(content string) [16]byte
}

// UpdateService wraps release checks and upgrades.
type UpdateService interface {
	Check() (*update.CheckResult, error)
	Upgrade(release *update.Release) error
	IsHomebrewBuild() bool
}
