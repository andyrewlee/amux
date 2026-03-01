// Package service provides a UI-independent service layer for Medusa.
// It encapsulates all business logic for managing projects, workspaces,
// agent tabs, and configuration — decoupled from any specific UI framework.
//
// The service layer is used by:
//   - The HTTP server (REST + WebSocket + SSE handlers)
//   - The TUI client (via API client in the future)
//
// Key services:
//   - ProjectService: git repository management
//   - WorkspaceService: git worktree workspace lifecycle
//   - ClaudeService: Claude Code structured JSONL interaction
//   - AgentService: PTY-based agent fallback (Codex, Gemini, etc.)
//   - ConfigService: settings, profiles, permissions
//   - GroupService: multi-repo project groups
//   - SessionStore: conversation persistence for cross-device sync
//   - EventBus: typed pub/sub for real-time state changes
package service

import (
	"fmt"
	"path/filepath"

	"github.com/andyrewlee/medusa/internal/config"
	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/logging"
	"github.com/andyrewlee/medusa/internal/process"
)

// Services is the top-level container holding all service instances.
// It wires together shared dependencies (config, stores, event bus)
// and provides a single point for initialization and shutdown.
type Services struct {
	EventBus  *EventBus
	Sessions  *SessionStore
	Projects  *ProjectService
	Workspaces *WorkspaceService
	Claude    *ClaudeService
	Agents    *AgentService
	Config    *ConfigService
	Groups    *GroupService
}

// New creates and initializes all services from configuration.
func New(cfg *config.Config) (*Services, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	// Ensure required directories exist
	if err := cfg.Paths.EnsureDirectories(); err != nil {
		return nil, fmt.Errorf("ensuring directories: %w", err)
	}

	// Shared components
	eventBus := NewEventBus()
	registry := data.NewRegistry(cfg.Paths.RegistryPath)
	workspaceStore := data.NewWorkspaceStore(cfg.Paths.MetadataRoot)
	scripts := process.NewScriptRunner(cfg.PortStart, cfg.PortRangeSize)

	sessionsDir := filepath.Join(filepath.Dir(cfg.Paths.RegistryPath), "sessions")
	sessionStore := NewSessionStore(sessionsDir)

	// Create services
	projectSvc := NewProjectService(registry, workspaceStore, cfg, eventBus)
	workspaceSvc := NewWorkspaceService(registry, workspaceStore, cfg, scripts, eventBus)
	claudeSvc := NewClaudeService(cfg, sessionStore, eventBus)
	agentSvc := NewAgentService(cfg, eventBus)
	configSvc := NewConfigService(cfg, registry, eventBus)
	groupSvc := NewGroupService(registry, workspaceStore, cfg, eventBus)

	services := &Services{
		EventBus:   eventBus,
		Sessions:   sessionStore,
		Projects:   projectSvc,
		Workspaces: workspaceSvc,
		Claude:     claudeSvc,
		Agents:     agentSvc,
		Config:     configSvc,
		Groups:     groupSvc,
	}

	// Initialize session store (load metadata for resumable sessions)
	if err := sessionStore.Init(); err != nil {
		logging.Warn("Failed to initialize session store: %v", err)
	}

	// Initialize Claude service (load resumable sessions)
	if err := claudeSvc.Init(); err != nil {
		logging.Warn("Failed to initialize Claude service: %v", err)
	}

	return services, nil
}

// Shutdown gracefully stops all services.
func (s *Services) Shutdown() {
	if s.Claude != nil {
		s.Claude.Shutdown()
	}
	if s.Agents != nil {
		s.Agents.Shutdown()
	}
}
