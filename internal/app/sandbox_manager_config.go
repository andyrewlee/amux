package app

import (
	"strings"

	"github.com/andyrewlee/amux/internal/tmux"
)

func (m *SandboxManager) SetTmuxOptions(opts tmux.Options) {
	m.mu.Lock()
	m.tmuxOptions = opts
	m.mu.Unlock()
}

func (m *SandboxManager) SetInstanceID(id string) {
	m.mu.Lock()
	m.instanceID = strings.TrimSpace(id)
	m.mu.Unlock()
}

func (m *SandboxManager) SetShellDetachedCallback(fn func(workspaceID string)) {
	m.mu.Lock()
	m.shellDetachedFn = fn
	m.mu.Unlock()
}
