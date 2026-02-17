package app

import (
	"context"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/update"
)

// ProjectRegistry is the minimal interface used by the app for project tracking.
type ProjectRegistry interface {
	Projects() ([]string, error)
	AddProject(path string) error
	RemoveProject(path string) error
}

// WorkspaceStore is the minimal interface used by the app for workspace metadata.
type WorkspaceStore interface {
	ListByRepo(repo string) ([]*data.Workspace, error)
	ListByRepoIncludingArchived(repo string) ([]*data.Workspace, error)
	LoadMetadataFor(workspace *data.Workspace) (bool, error)
	UpsertFromDiscovery(workspace *data.Workspace) error
	Save(workspace *data.Workspace) error
	Delete(id data.WorkspaceID) error
	ResolvedDefaultAssistant() string
}

// GitStatusService provides cached status reads and fresh refreshes.
type GitStatusService interface {
	Run(ctx context.Context) error
	GetCached(root string) *git.StatusResult
	UpdateCache(root string, status *git.StatusResult)
	Invalidate(root string)
	Refresh(root string) (*git.StatusResult, error)
}

// TmuxOps defines tmux operations used by the app.
type TmuxOps interface {
	EnsureAvailable() error
	InstallHint() string
	ActiveAgentSessionsByActivity(window time.Duration, opts tmux.Options) ([]tmux.SessionActivity, error)
	SessionsWithTags(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error)
	SessionStateFor(sessionName string, opts tmux.Options) (tmux.SessionState, error)
	SessionHasClients(sessionName string, opts tmux.Options) (bool, error)
	SessionCreatedAt(sessionName string, opts tmux.Options) (int64, error)
	KillSession(sessionName string, opts tmux.Options) error
	KillSessionsMatchingTags(tags map[string]string, opts tmux.Options) (bool, error)
	KillSessionsWithPrefix(prefix string, opts tmux.Options) error
	KillWorkspaceSessions(wsID string, opts tmux.Options) error
	SetMonitorActivityOn(opts tmux.Options) error
	SetStatusOff(opts tmux.Options) error
	CapturePaneTail(sessionName string, lines int, opts tmux.Options) (string, bool)
	ContentHash(content string) [16]byte
}

// UpdateService wraps release checks and upgrades.
type UpdateService interface {
	Check() (*update.CheckResult, error)
	Upgrade(release *update.Release) error
	IsHomebrewBuild() bool
}
