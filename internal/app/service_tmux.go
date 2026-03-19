package app

import (
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// tmuxOps is the default implementation backed by the tmux package.
type tmuxOps struct{}

func (tmuxOps) EnsureAvailable() error {
	return tmux.EnsureAvailable()
}

func (tmuxOps) InstallHint() string {
	return tmux.InstallHint()
}

func (tmuxOps) ActiveAgentSessionsByActivity(window time.Duration, opts tmux.Options) ([]tmux.SessionActivity, error) {
	return tmux.ActiveAgentSessionsByActivity(window, opts)
}

func (tmuxOps) SessionsWithTags(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
	return tmux.SessionsWithTags(match, keys, opts)
}

func (tmuxOps) AllSessionStates(opts tmux.Options) (map[string]tmux.SessionState, error) {
	return tmux.AllSessionStates(opts)
}

func (tmuxOps) SessionStateFor(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
	return tmux.SessionStateFor(sessionName, opts)
}

func (tmuxOps) SessionHasClients(sessionName string, opts tmux.Options) (bool, error) {
	return tmux.SessionHasClients(sessionName, opts)
}

func (tmuxOps) SessionNamesWithClients(opts tmux.Options) (map[string]bool, error) {
	return tmux.SessionNamesWithClients(opts)
}

func (tmuxOps) SessionCreatedAt(sessionName string, opts tmux.Options) (int64, error) {
	return tmux.SessionCreatedAt(sessionName, opts)
}

func (tmuxOps) KillSession(sessionName string, opts tmux.Options) error {
	return tmux.KillSession(sessionName, opts)
}

func (tmuxOps) KillSessionsMatchingTags(tags map[string]string, opts tmux.Options) (bool, error) {
	return tmux.KillSessionsMatchingTags(tags, opts)
}

func (tmuxOps) KillSessionsWithPrefix(prefix string, opts tmux.Options) error {
	return tmux.KillSessionsWithPrefix(prefix, opts)
}

func (tmuxOps) KillWorkspaceSessions(wsID string, opts tmux.Options) error {
	return tmux.KillWorkspaceSessions(wsID, opts)
}

func (tmuxOps) SetMonitorActivityOn(opts tmux.Options) error {
	return tmux.SetMonitorActivityOn(opts)
}

func (tmuxOps) SetStatusOff(opts tmux.Options) error {
	return tmux.SetStatusOff(opts)
}

func (tmuxOps) CapturePaneTail(sessionName string, lines int, opts tmux.Options) (string, bool) {
	return tmux.CapturePaneTail(sessionName, lines, opts)
}

func (tmuxOps) ContentHash(content string) [16]byte {
	return tmux.ContentHash(content)
}
