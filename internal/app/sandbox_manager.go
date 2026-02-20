package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/sandbox"
)

const defaultSandboxAutoStopMinutes int32 = 30

type sandboxSession struct {
	sandbox          sandbox.RemoteSandbox
	provider         sandbox.Provider
	providerName     string
	worktreeID       string
	workspacePath    string
	synced           bool
	credentialsReady bool
}

// SandboxManager coordinates per-worktree sandbox sessions for the TUI.
type SandboxManager struct {
	cfg      *config.Config
	mu       sync.Mutex
	sessions map[string]*sandboxSession
}

func NewSandboxManager(cfg *config.Config) *SandboxManager {
	return &SandboxManager{
		cfg:      cfg,
		sessions: make(map[string]*sandboxSession),
	}
}

func (m *SandboxManager) ensureProvider(wt *data.Workspace) (sandbox.Provider, string, sandbox.Config, error) {
	cfg, err := sandbox.LoadConfig()
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
	provider, name, err := sandbox.ResolveProvider(cfg, wt.Root, "")
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

func (m *SandboxManager) storeSession(session *sandboxSession) {
	if session == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[session.worktreeID] = session
}

func (m *SandboxManager) attachSession(wt *data.Workspace) (*sandboxSession, error) {
	if wt == nil {
		return nil, errors.New("workspace is required")
	}
	worktreeID := sandbox.ComputeWorktreeID(wt.Root)
	if existing := m.sessionFor(worktreeID); existing != nil {
		return existing, nil
	}

	provider, providerName, _, err := m.ensureProvider(wt)
	if err != nil {
		return nil, err
	}

	meta, err := sandbox.LoadSandboxMeta(wt.Root, providerName)
	if err != nil || meta == nil || meta.SandboxID == "" {
		return nil, nil
	}

	sb, err := provider.GetSandbox(context.Background(), meta.SandboxID)
	if err != nil {
		return nil, nil
	}
	if err := sb.Start(context.Background()); err != nil {
		return nil, err
	}
	if err := sb.WaitReady(context.Background(), 60*time.Second); err != nil {
		logging.Warn("Sandbox attach wait ready: %v", err)
	}

	workspacePath := sandbox.GetWorktreeRepoPath(sb, sandbox.SyncOptions{
		Cwd:        wt.Root,
		WorktreeID: worktreeID,
	})

	session := &sandboxSession{
		sandbox:       sb,
		provider:      provider,
		providerName:  providerName,
		worktreeID:    worktreeID,
		workspacePath: workspacePath,
		synced:        true,
	}

	// Ensure persistence wiring for credentials.
	if err := sandbox.SetupCredentials(sb, sandbox.CredentialsConfig{
		Mode:             "auto",
		SettingsSyncMode: "auto",
	}, false); err != nil {
		return nil, err
	}
	session.credentialsReady = true
	m.storeSession(session)
	return session, nil
}

func (m *SandboxManager) ensureSession(wt *data.Workspace, agent sandbox.Agent) (*sandboxSession, error) {
	if wt == nil {
		return nil, errors.New("workspace is required")
	}
	worktreeID := sandbox.ComputeWorktreeID(wt.Root)
	if existing := m.sessionFor(worktreeID); existing != nil {
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
		workspacePath: workspacePath,
	}

	if err := sandbox.UploadWorkspace(sb, sandbox.SyncOptions{
		Cwd:        wt.Root,
		WorktreeID: worktreeID,
	}, false); err != nil {
		return nil, err
	}
	session.synced = true

	if err := sandbox.SetupCredentials(sb, sandbox.CredentialsConfig{
		Mode:             "auto",
		SettingsSyncMode: "auto",
	}, false); err != nil {
		return nil, err
	}
	session.credentialsReady = true

	m.storeSession(session)
	return session, nil
}

func (m *SandboxManager) CreateAgent(wt *data.Workspace, agentType pty.AgentType, rows, cols uint16) (*pty.Agent, error) {
	agent := sandbox.Agent(agentType)
	if !sandbox.IsValidAgent(agent.String()) {
		return nil, fmt.Errorf("unknown agent type: %s", agentType)
	}

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

	cmd, cleanup, err := sandbox.BuildSSHCommand(session.sandbox, remoteCommand)
	if err != nil {
		return nil, err
	}

	term, err := pty.NewWithCmd(cmd, cleanup)
	if err != nil {
		return nil, err
	}

	// Set initial size if provided
	if rows > 0 && cols > 0 {
		_ = term.SetSize(rows, cols)
	}

	assistantCfg := m.cfg.Assistants[string(agentType)]
	return &pty.Agent{
		Type:      agentType,
		Terminal:  term,
		Workspace: wt,
		Config:    assistantCfg,
	}, nil
}

func (m *SandboxManager) CreateViewer(wt *data.Workspace, command string, rows, cols uint16) (*pty.Agent, error) {
	session, err := m.ensureSession(wt, sandbox.AgentShell)
	if err != nil {
		return nil, err
	}

	remoteCommand := buildRemoteCommand(session.workspacePath, command, map[string]string{
		"WORKTREE_ROOT": session.workspacePath,
		"WORKTREE_NAME": wt.Name,
	})
	cmd, cleanup, err := sandbox.BuildSSHCommand(session.sandbox, remoteCommand)
	if err != nil {
		return nil, err
	}

	term, err := pty.NewWithCmd(cmd, cleanup)
	if err != nil {
		return nil, err
	}

	// Set initial size if provided
	if rows > 0 && cols > 0 {
		_ = term.SetSize(rows, cols)
	}

	return &pty.Agent{
		Type:      pty.AgentType("viewer"),
		Terminal:  term,
		Workspace: wt,
	}, nil
}

func (m *SandboxManager) CreateShell(wt *data.Workspace) (*pty.Terminal, error) {
	session, err := m.ensureSession(wt, sandbox.AgentShell)
	if err != nil {
		return nil, err
	}
	remoteCommand := buildRemoteCommand(session.workspacePath, "exec bash -i", map[string]string{
		"WORKTREE_ROOT": session.workspacePath,
		"WORKTREE_NAME": wt.Name,
	})
	cmd, cleanup, err := sandbox.BuildSSHCommand(session.sandbox, remoteCommand)
	if err != nil {
		return nil, err
	}
	return pty.NewWithCmd(cmd, cleanup)
}

func (m *SandboxManager) SyncToLocal(wt *data.Workspace) error {
	session, err := m.attachSession(wt)
	if err != nil || session == nil {
		return err
	}
	return sandbox.DownloadWorkspace(session.sandbox, sandbox.SyncOptions{
		Cwd:        wt.Root,
		WorktreeID: session.worktreeID,
	}, false)
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
