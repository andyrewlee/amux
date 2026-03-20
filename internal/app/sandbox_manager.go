package app

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/sandbox"
	"github.com/andyrewlee/amux/internal/tmux"
)

const defaultSandboxAutoStopMinutes int32 = 30

var (
	errSandboxSyncConflict = errors.New("sandbox sync would overwrite local changes")
	errSandboxSyncLive     = errors.New("sandbox session is still active")

	loadSandboxConfig       = sandbox.LoadConfig
	resolveSandboxProvider  = sandbox.ResolveProvider
	loadSandboxMeta         = sandbox.LoadSandboxMeta
	setupSandboxCredentials = sandbox.SetupCredentials
)

type sandboxSession struct {
	sandbox            sandbox.RemoteSandbox
	provider           sandbox.Provider
	providerName       string
	worktreeID         string
	workspaceID        data.WorkspaceID
	workspaceIDAliases map[string]struct{}
	workspaceRepo      string
	workspaceRoot      string
	workspacePath      string
	tmuxSessionNames   map[string]struct{}
	activeShells       int
	needsSyncDown      bool
	credentialsReady   bool
}

// SandboxManager coordinates per-worktree sandbox sessions for the TUI.
type SandboxManager struct {
	cfg                 *config.Config
	mu                  sync.Mutex
	sessions            map[string]*sandboxSession
	instanceID          string
	tmuxOptions         tmux.Options
	attachSessionFn     func(wt *data.Workspace) (*sandboxSession, error)
	buildSSHCommand     func(sb sandbox.RemoteSandbox, remoteCommand string) (*exec.Cmd, func(), error)
	sessionsWithTags    func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error)
	setSessionTagValues func(sessionName string, tags []tmux.OptionValue, opts tmux.Options) error
	sessionStateFor     func(sessionName string, opts tmux.Options) (tmux.SessionState, error)
	attachTmuxTerminal  func(sessionName string, rows, cols uint16, opts tmux.Options) (*pty.Terminal, error)
	killTmuxSession     func(sessionName string, opts tmux.Options) error
	downloadWorkspace   func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error
	shellDetachedFn     func(workspaceID string)
	launchPollInterval  time.Duration
	launchWatchTimeout  time.Duration
	shutdownCtx         context.Context
	shutdownCancel      context.CancelFunc
}

func NewSandboxManager(cfg *config.Config) *SandboxManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &SandboxManager{
		cfg:                 cfg,
		sessions:            make(map[string]*sandboxSession),
		tmuxOptions:         tmux.DefaultOptions(),
		buildSSHCommand:     sandbox.BuildSSHCommand,
		sessionsWithTags:    tmux.SessionsWithTags,
		setSessionTagValues: tmux.SetSessionTagValues,
		sessionStateFor:     tmux.SessionStateFor,
		attachTmuxTerminal:  defaultAttachTmuxTerminal,
		killTmuxSession:     tmux.KillSession,
		downloadWorkspace:   sandbox.DownloadWorkspace,
		launchPollInterval:  100 * time.Millisecond,
		launchWatchTimeout:  5 * time.Second,
		shutdownCtx:         ctx,
		shutdownCancel:      cancel,
	}
}

func (m *SandboxManager) getTmuxOptions() tmux.Options {
	m.mu.Lock()
	opts := m.tmuxOptions
	m.mu.Unlock()
	return opts
}

func (m *SandboxManager) ensureProvider(wt *data.Workspace) (sandbox.Provider, string, sandbox.Config, error) {
	return m.ensureProviderWithOverride(wt, "")
}

func (m *SandboxManager) ensureProviderWithOverride(wt *data.Workspace, providerOverride string) (sandbox.Provider, string, sandbox.Config, error) {
	cfg, err := loadSandboxConfig()
	if err != nil {
		return nil, "", cfg, err
	}
	if sandbox.ResolveAPIKey(cfg) == "" {
		return nil, "", cfg, sandbox.NewSandboxError(
			sandbox.ErrCodeConfig,
			"auth",
			errors.New("Daytona API key not found"),
		)
	}
	provider, name, err := resolveSandboxProvider(cfg, wt.Root, providerOverride)
	if err != nil {
		return nil, name, cfg, err
	}
	return provider, name, cfg, nil
}

func (m *SandboxManager) sessionFor(worktreeID string) *sandboxSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[worktreeID]
}

func (m *SandboxManager) sessionForWorkspace(wt *data.Workspace) *sandboxSession {
	return m.sessionForWorkspaceProvider(wt, "")
}

func (m *SandboxManager) sessionForWorkspaceProvider(wt *data.Workspace, providerName string) *sandboxSession {
	if wt == nil {
		return nil
	}
	worktreeID := sandbox.ComputeWorktreeID(wt.Root)
	m.mu.Lock()
	defer m.mu.Unlock()
	if session := m.sessions[worktreeID]; session != nil && sessionMatchesProvider(session, providerName) {
		return session
	}
	targetRoot := canonicalPathForMatch(wt.Root)
	if targetRoot == "" {
		return nil
	}
	for _, session := range m.sessions {
		if session == nil {
			continue
		}
		if !sessionMatchesProvider(session, providerName) {
			continue
		}
		if canonicalPathForMatch(session.workspaceRoot) == targetRoot {
			return session
		}
	}
	return nil
}

func sessionMatchesProvider(session *sandboxSession, providerName string) bool {
	if session == nil || providerName == "" {
		return session != nil
	}
	return normalizeSandboxProviderName(session.providerName) == providerName
}

func normalizeSandboxProviderName(providerName string) string {
	if name := strings.TrimSpace(providerName); name != "" {
		return name
	}
	return sandbox.DefaultProviderName
}

func (m *SandboxManager) storeSession(session *sandboxSession) {
	if session == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[session.worktreeID] = session
}

func (m *SandboxManager) rollbackFailedSessionInit(session *sandboxSession, workspaceRoot string, cause error) {
	if session == nil || session.sandbox == nil || session.provider == nil {
		return
	}

	m.mu.Lock()
	activeSession := m.sessions[session.worktreeID]
	m.mu.Unlock()
	if activeSession != nil && activeSession != session {
		logging.Warn("Sandbox init rollback: skipping teardown for worktree %q after %v because another session is active", session.worktreeID, cause)
		return
	}

	sandboxID := strings.TrimSpace(session.sandbox.ID())
	if err := session.sandbox.Stop(context.Background()); err != nil {
		logging.Warn("Sandbox init rollback: failed to stop sandbox %q after %v: %v", sandboxID, cause, err)
	}
	if sandboxID != "" {
		metaSafeToRemove := false
		if err := session.provider.DeleteSandbox(context.Background(), sandboxID); err != nil {
			logging.Warn("Sandbox init rollback: failed to delete sandbox %q after %v: %v", sandboxID, cause, err)
			metaSafeToRemove = sandbox.IsNotFoundError(err)
		} else {
			metaSafeToRemove = true
		}
		if metaSafeToRemove {
			if err := sandbox.RemoveSandboxMetaByID(sandboxID); err != nil {
				logging.Warn("Sandbox init rollback: failed to remove metadata for sandbox %q after %v: %v", sandboxID, cause, err)
			}
		}
	}
	if sandboxID == "" {
		if err := sandbox.RemoveSandboxMeta(workspaceRoot, session.providerName); err != nil {
			logging.Warn("Sandbox init rollback: failed to remove metadata for workspace %q after %v: %v", workspaceRoot, cause, err)
		}
	}

	m.mu.Lock()
	if active := m.sessions[session.worktreeID]; active == session {
		delete(m.sessions, session.worktreeID)
	}
	m.mu.Unlock()
}

func (m *SandboxManager) attachSession(wt *data.Workspace) (*sandboxSession, error) {
	if wt == nil {
		return nil, errors.New("workspace is required")
	}
	m.mu.Lock()
	attachOverride := m.attachSessionFn
	m.mu.Unlock()
	if attachOverride != nil {
		return attachOverride(wt)
	}
	worktreeID := sandbox.ComputeWorktreeID(wt.Root)
	if existing := m.sessionForWorkspaceProvider(wt, selectedSandboxSessionProviderFilter()); existing != nil && existing.sandbox != nil {
		m.refreshSessionWorkspace(existing, wt)
		return existing, nil
	}

	cfg, err := loadSandboxConfig()
	if err != nil {
		return nil, err
	}
	providerName := sandbox.ResolveProviderName(cfg, "")
	meta, err := loadSandboxMeta(wt.Root, providerName)
	if err != nil || meta == nil || meta.SandboxID == "" {
		return nil, err
	}
	session := m.sessionFor(worktreeID)
	session = m.hydrateSessionFromMeta(session, wt, meta)
	provider, _, _, err := m.ensureProviderWithOverride(wt, "")
	if err != nil {
		return nil, err
	}

	sb, err := provider.GetSandbox(context.Background(), meta.SandboxID)
	if err != nil {
		if sandbox.IsNotFoundError(err) {
			return nil, nil
		}
		return nil, err
	}
	if err := sb.Start(context.Background()); err != nil {
		return nil, err
	}
	if err := sb.WaitReady(context.Background(), 60*time.Second); err != nil {
		logging.Warn("Sandbox attach wait ready: %v", err)
	}

	workspacePath := sessionWorkspacePath(sb, wt, session)

	session.sandbox = sb
	session.provider = provider
	session.providerName = providerName
	session.workspaceID = wt.ID()
	session.workspaceRepo = wt.Repo
	session.workspaceRoot = wt.Root
	session.workspacePath = workspacePath
	m.rememberSessionWorkspaceID(session, wt.ID())

	// Ensure persistence wiring for credentials.
	if !session.credentialsReady {
		if err := setupSandboxCredentials(sb, sandbox.CredentialsConfig{
			Mode:             "auto",
			SettingsSyncMode: "auto",
		}, false); err != nil {
			return nil, err
		}
		session.credentialsReady = true
	}
	if err := m.discoverTrackedTmuxSessions(session); err != nil {
		return nil, err
	}
	m.storeSession(session)
	return session, nil
}

func sessionWorkspacePath(sb sandbox.RemoteSandbox, wt *data.Workspace, session *sandboxSession) string {
	if sb == nil || wt == nil {
		return ""
	}
	worktreeID := sandbox.ComputeWorktreeID(wt.Root)
	if session != nil && strings.TrimSpace(session.worktreeID) != "" {
		worktreeID = session.worktreeID
	}
	return sandbox.GetWorktreeRepoPath(sb, sandbox.SyncOptions{
		Cwd:        wt.Root,
		WorktreeID: worktreeID,
	})
}

func (m *SandboxManager) ensureSession(wt *data.Workspace, agent sandbox.Agent) (*sandboxSession, error) {
	if wt == nil {
		return nil, errors.New("workspace is required")
	}
	worktreeID := sandbox.ComputeWorktreeID(wt.Root)
	if existing := m.sessionForWorkspaceProvider(wt, selectedSandboxSessionProviderFilter()); existing != nil && existing.sandbox != nil {
		m.refreshSessionWorkspace(existing, wt)
		return existing, nil
	}

	session, err := m.attachSession(wt)
	if err != nil {
		return nil, err
	}
	if session != nil {
		return session, nil
	}

	provider, providerName, cfg, err := m.ensureProvider(wt)
	if err != nil {
		return nil, err
	}

	if !sandbox.IsValidAgent(agent.String()) {
		agent = sandbox.AgentShell
	}

	sb, _, err := sandbox.CreateSandboxSession(provider, wt.Root, sandbox.SandboxConfig{
		Agent:                 agent,
		EnvVars:               nil,
		Volumes:               nil,
		AutoStopInterval:      defaultSandboxAutoStopMinutes,
		Snapshot:              sandbox.ResolveSnapshotID(cfg),
		Ephemeral:             false,
		PersistenceVolumeName: sandbox.ResolvePersistenceVolumeName(cfg),
	})
	if err != nil {
		return nil, err
	}

	workspacePath := sandbox.GetWorktreeRepoPath(sb, sandbox.SyncOptions{
		Cwd:        wt.Root,
		WorktreeID: worktreeID,
	})

	session = &sandboxSession{
		sandbox:       sb,
		provider:      provider,
		providerName:  providerName,
		worktreeID:    worktreeID,
		workspaceID:   wt.ID(),
		workspaceRoot: wt.Root,
		workspacePath: workspacePath,
	}

	if err := sandbox.UploadWorkspace(sb, sandbox.SyncOptions{
		Cwd:        wt.Root,
		WorktreeID: worktreeID,
	}, false); err != nil {
		m.rollbackFailedSessionInit(session, wt.Root, err)
		return nil, err
	}
	m.setSessionNeedsSync(session, false)

	if err := sandbox.SetupCredentials(sb, sandbox.CredentialsConfig{
		Mode:             "auto",
		SettingsSyncMode: "auto",
	}, false); err != nil {
		m.rollbackFailedSessionInit(session, wt.Root, err)
		return nil, err
	}
	session.credentialsReady = true

	m.storeSession(session)
	return session, nil
}

func (m *SandboxManager) CreateAgent(wt *data.Workspace, agentType pty.AgentType, rows, cols uint16) (*pty.Agent, error) {
	return m.CreateAgentWithTags(wt, agentType, "", rows, cols, tmux.SessionTags{})
}

func (m *SandboxManager) CreateAgentWithTags(wt *data.Workspace, agentType pty.AgentType, sessionName string, rows, cols uint16, tags tmux.SessionTags) (*pty.Agent, error) {
	agent := sandbox.Agent(agentType)
	if !sandbox.IsValidAgent(agent.String()) {
		return nil, fmt.Errorf("unknown agent type: %s", agentType)
	}
	tags.Runtime = string(data.RuntimeCloudSandbox)

	session, err := m.ensureSession(wt, agent)
	if err != nil {
		return nil, err
	}

	if err := sandbox.EnsureAgentInstalled(session.sandbox, agent, false, false); err != nil {
		return nil, err
	}

	env := map[string]string{
		"WORKTREE_ROOT": session.workspacePath,
		"WORKTREE_NAME": wt.Name,
	}
	remoteCommand, err := sandbox.BuildAgentRemoteCommand(session.sandbox, sandbox.AgentConfig{
		Agent:         agent,
		WorkspacePath: session.workspacePath,
		Env:           env,
	})
	if err != nil {
		return nil, err
	}

	term, err := m.newTmuxBackedSandboxAgent(wt, session, agentType, sessionName, remoteCommand, rows, cols, tags)
	if err != nil {
		return nil, err
	}
	m.setSessionNeedsSync(session, true)

	assistantCfg := m.cfg.Assistants[string(agentType)]
	return &pty.Agent{
		Type:      agentType,
		Terminal:  term,
		Workspace: wt,
		Config:    assistantCfg,
		Session:   tmuxSessionName(wt, agentType, sessionName),
	}, nil
}

func (m *SandboxManager) CreateViewer(wt *data.Workspace, command string, rows, cols uint16) (*pty.Agent, error) {
	return m.CreateViewerWithTags(wt, command, "", rows, cols, tmux.SessionTags{})
}

func (m *SandboxManager) CreateViewerWithTags(wt *data.Workspace, command, sessionName string, rows, cols uint16, tags tmux.SessionTags) (*pty.Agent, error) {
	tags.Runtime = string(data.RuntimeCloudSandbox)
	session, err := m.ensureSession(wt, sandbox.AgentShell)
	if err != nil {
		return nil, err
	}

	remoteCommand := buildRemoteCommand(session.workspacePath, command, map[string]string{
		"WORKTREE_ROOT": session.workspacePath,
		"WORKTREE_NAME": wt.Name,
	})
	term, err := m.newTmuxBackedSandboxViewer(wt, session, command, sessionName, remoteCommand, rows, cols, tags)
	if err != nil {
		return nil, err
	}
	m.setSessionNeedsSync(session, true)

	return &pty.Agent{
		Type:      pty.AgentType("viewer"),
		Terminal:  term,
		Workspace: wt,
		Session:   tmuxSessionNameForViewer(wt, sessionName),
	}, nil
}

func (m *SandboxManager) CreateShell(wt *data.Workspace) (*pty.Terminal, error) {
	session, err := m.ensureSession(wt, sandbox.AgentShell)
	if err != nil {
		return nil, err
	}
	m.trackShellAttach(session)
	remoteCommand := buildRemoteCommand(session.workspacePath, "exec bash -i", map[string]string{
		"WORKTREE_ROOT": session.workspacePath,
		"WORKTREE_NAME": wt.Name,
	})
	cmd, cleanup, err := m.buildSSHCommand(session.sandbox, remoteCommand)
	if err != nil {
		m.trackShellDetach(session)
		return nil, err
	}
	term, err := pty.NewWithCmd(cmd, func() {
		m.trackShellDetach(session)
		if cleanup != nil {
			cleanup()
		}
		m.notifyShellDetached(session)
	})
	if err != nil {
		m.trackShellDetach(session)
		return nil, err
	}
	m.setSessionNeedsSync(session, true)
	return term, nil
}

func (m *SandboxManager) GitStatus(wt *data.Workspace) (*git.StatusResult, error) {
	session, err := m.ensureSession(wt, sandbox.AgentShell)
	if err != nil {
		return nil, err
	}
	resp, err := session.sandbox.Exec(context.Background(), "git status --short", &sandbox.ExecOptions{
		Cwd: session.workspacePath,
	})
	if err != nil {
		return nil, err
	}
	if resp.ExitCode != 0 {
		return nil, fmt.Errorf("git status failed: %s", resp.Stdout)
	}
	return git.ParseStatus(resp.Stdout), nil
}

// CancelLaunchPollers cancels background launch-token cleanup goroutines.
func (m *SandboxManager) CancelLaunchPollers() {
	m.mu.Lock()
	cancel := m.shutdownCancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func buildRemoteCommand(workspacePath, command string, env map[string]string) string {
	parts := make([]string, 0, 3)
	if len(env) > 0 {
		parts = append(parts, sandbox.BuildEnvExports(env)...)
	}
	if workspacePath != "" {
		parts = append(parts, "cd "+sandbox.ShellQuote(workspacePath))
	}
	parts = append(parts, command)
	script := strings.Join(parts, "\n")
	return "bash -lc " + sandbox.ShellQuote(script)
}
