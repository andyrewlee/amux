package pty

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/andyrewlee/medusa/internal/config"
	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/git"
	"github.com/andyrewlee/medusa/internal/sandbox"
	"github.com/andyrewlee/medusa/internal/tmux"
)

// AgentOptions holds optional flags for agent creation.
type AgentOptions struct {
	ClaudeSessionID string // UUID to pass as --session-id or --resume
	Resume          bool   // If true, use --resume instead of --session-id
}

// GenerateSessionID returns a new random UUID v4 string.
func GenerateSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	var buf [36]byte
	hex.Encode(buf[0:8], b[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], b[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], b[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], b[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], b[10:16])
	return string(buf[:])
}

// AgentType represents the type of AI agent
type AgentType string

const (
	AgentClaude   AgentType = "claude"
	AgentCodex    AgentType = "codex"
	AgentGemini   AgentType = "gemini"
	AgentAmp      AgentType = "amp"
	AgentOpencode AgentType = "opencode"
	AgentDroid    AgentType = "droid"
	AgentCursor   AgentType = "cursor"
)

// Agent represents a running AI agent instance
type Agent struct {
	Type      AgentType
	Terminal  *Terminal
	Workspace *data.Workspace
	Config    config.AssistantConfig
	Session   string
}

// AgentManager manages agent instances
type AgentManager struct {
	config *config.Config
	mu     sync.Mutex
	agents map[data.WorkspaceID][]*Agent
}

// NewAgentManager creates a new agent manager
func NewAgentManager(cfg *config.Config) *AgentManager {
	return &AgentManager{
		config: cfg,
		agents: make(map[data.WorkspaceID][]*Agent),
	}
}

// CreateAgent creates a new agent for the given workspace.
func (m *AgentManager) CreateAgent(ws *data.Workspace, agentType AgentType, sessionName string, rows, cols uint16) (*Agent, error) {
	return m.CreateAgentWithTags(ws, agentType, sessionName, rows, cols, tmux.SessionTags{}, AgentOptions{})
}

// CreateAgentWithTags creates a new agent for the given workspace with tmux tags.
func (m *AgentManager) CreateAgentWithTags(ws *data.Workspace, agentType AgentType, sessionName string, rows, cols uint16, tags tmux.SessionTags, opts AgentOptions) (*Agent, error) {
	if agentType == AgentClaude && ws.Profile == "" {
		return nil, fmt.Errorf("cannot start Claude agent without a profile (workspace %q)", ws.Name)
	}
	assistantCfg, ok := m.config.Assistants[string(agentType)]
	if !ok {
		return nil, fmt.Errorf("unknown agent type: %s", agentType)
	}
	if sessionName == "" {
		sessionName, _ = tmux.NextUniqueSessionName(ws.Name, tmux.DefaultOptions())
	}
	if err := tmux.EnsureAvailable(); err != nil {
		return nil, err
	}

	// Build environment
	env := []string{
		fmt.Sprintf("WORKSPACE_ROOT=%s", ws.Root),
		fmt.Sprintf("WORKSPACE_NAME=%s", ws.Name),
		"LINES=",   // Unset to force ioctl usage
		"COLUMNS=", // Unset to force ioctl usage
		"COLORTERM=truecolor",
	}

	// Set CLAUDE_CONFIG_DIR for Claude agents with a named profile.
	// Prefix the agent command directly so it propagates into the tmux session.
	agentCommand := assistantCfg.Command
	var profileDir string
	if agentType == AgentClaude && ws.Profile != "" {
		profileDir = filepath.Join(m.config.Paths.ProfilesRoot, ws.Profile)
		_ = os.MkdirAll(profileDir, 0755)
		if m.config.UI.SyncProfilePlugins {
			_ = config.SyncProfileSharedDirs(m.config.Paths.ProfilesRoot, ws.Profile)
		}
		// Inject global permissions into the profile if enabled
		if m.config.UI.GlobalPermissions {
			global, err := config.LoadGlobalPermissions(m.config.Paths.GlobalPermissionsPath)
			if err == nil && (len(global.Allow) > 0 || len(global.Deny) > 0) {
				_ = config.InjectGlobalPermissions(profileDir, global)
			}
		}
		// Inject Edit permission if workspace has AllowEdits enabled
		if ws.AllowEdits {
			_ = config.InjectAllowEdits(ws.Root)
		}
		agentCommand = fmt.Sprintf("CLAUDE_CONFIG_DIR=%s %s", shellQuote(profileDir), agentCommand)
	}

	// Pre-trust the workspace directory so Claude doesn't prompt
	// Use profile config dir if set, otherwise default ~/.claude.json
	if agentType == AgentClaude {
		_ = config.InjectTrustedDirectory(ws.Root, profileDir)
	}

	// Append Claude session flags for conversation resumption.
	if agentType == AgentClaude && opts.ClaudeSessionID != "" {
		if opts.Resume {
			agentCommand += " --resume " + shellQuote(opts.ClaudeSessionID)
		} else {
			agentCommand += " --session-id " + shellQuote(opts.ClaudeSessionID)
		}
	}

	// Skip permissions: append --dangerously-skip-permissions independently of sandbox.
	var sbplCleanup func()
	if ws.SkipPermissions && agentType == AgentClaude {
		agentCommand += " --dangerously-skip-permissions"
		_ = config.InjectSkipPermissionPrompt(profileDir)
	}

	// Create terminal with agent command, falling back to shell on exit
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	// Execute agent, then reset terminal state and drop to shell
	// Reset sequence: stty sane (terminal modes), exit alt screen, show cursor, reset attrs, RIS
	// Use -l flag to start login shell so .zshrc/.bashrc are loaded
	fullCommand := fmt.Sprintf("%s; stty sane; printf '\\033[?1049l\\033[?25h\\033[0m\\033c'; echo 'Agent exited. Dropping to shell...'; export TERM=xterm-256color; exec %s -l", agentCommand, shell)

	// Wrap the entire command chain in sandbox-exec so the fallback shell
	// also runs inside the sandbox.
	if ws.Isolated {
		var gitDirs []string
		if gd, err := git.ResolveWorktreeGitDir(ws.Root); err == nil {
			gitDirs = append(gitDirs, gd)
		}
		sbpl := sandbox.GenerateSBPL(ws.Root, gitDirs, profileDir)
		sbplPath, cleanup, sErr := sandbox.WriteTempProfile(sbpl)
		if sErr == nil {
			sbplCleanup = cleanup
			fullCommand = sandbox.WrapCommand(fullCommand, sbplPath)
		}
	}

	termCommand := tmux.ClientCommandWithTags(sessionName, ws.Root, fullCommand, tmux.DefaultOptions(), tags)
	term, err := NewWithSize(termCommand, ws.Root, env, rows, cols)
	if err != nil {
		if sbplCleanup != nil {
			sbplCleanup()
		}
		return nil, fmt.Errorf("failed to create terminal: %w", err)
	}

	agent := &Agent{
		Type:      agentType,
		Terminal:  term,
		Workspace: ws,
		Config:    assistantCfg,
		Session:   sessionName,
	}

	m.mu.Lock()
	m.agents[ws.ID()] = append(m.agents[ws.ID()], agent)
	m.mu.Unlock()

	return agent, nil
}

// CreateGroupAgentWithTags creates a new agent for a group workspace with tmux tags.
// It injects additionalDirectories, trusts all roots, and sets up allow-edits for all roots.
func (m *AgentManager) CreateGroupAgentWithTags(
	gw *data.GroupWorkspace,
	agentType AgentType,
	sessionName string,
	rows, cols uint16,
	tags tmux.SessionTags,
	opts AgentOptions,
) (*Agent, error) {
	if agentType == AgentClaude && gw.Profile == "" {
		return nil, fmt.Errorf("cannot start Claude agent without a profile (group workspace %q)", gw.Name)
	}
	assistantCfg, ok := m.config.Assistants[string(agentType)]
	if !ok {
		return nil, fmt.Errorf("unknown agent type: %s", agentType)
	}
	if sessionName == "" {
		sessionName, _ = tmux.NextUniqueSessionName(gw.Name, tmux.DefaultOptions())
	}
	if err := tmux.EnsureAvailable(); err != nil {
		return nil, err
	}

	// Build environment — Primary.Root is the group workspace dir (parent of all repos)
	env := []string{
		fmt.Sprintf("WORKSPACE_ROOT=%s", gw.Primary.Root),
		fmt.Sprintf("WORKSPACE_NAME=%s", gw.Name),
		"LINES=",
		"COLUMNS=",
		"COLORTERM=truecolor",
	}

	agentCommand := assistantCfg.Command
	var profileDir string
	if agentType == AgentClaude && gw.Profile != "" {
		profileDir = filepath.Join(m.config.Paths.ProfilesRoot, gw.Profile)
		_ = os.MkdirAll(profileDir, 0755)
		if m.config.UI.SyncProfilePlugins {
			_ = config.SyncProfileSharedDirs(m.config.Paths.ProfilesRoot, gw.Profile)
		}
		if m.config.UI.GlobalPermissions {
			global, err := config.LoadGlobalPermissions(m.config.Paths.GlobalPermissionsPath)
			if err == nil && (len(global.Allow) > 0 || len(global.Deny) > 0) {
				_ = config.InjectGlobalPermissions(profileDir, global)
			}
		}
		agentCommand = fmt.Sprintf("CLAUDE_CONFIG_DIR=%s %s", shellQuote(profileDir), agentCommand)
	}

	// Trust the group workspace root and inject allow-edits at that level.
	// All repo worktrees are children of Primary.Root, so trusting the parent suffices.
	if agentType == AgentClaude {
		_ = config.InjectTrustedDirectory(gw.Primary.Root, profileDir)
		if gw.AllowEdits {
			_ = config.InjectAllowEdits(gw.Primary.Root)
		}
	}

	// Append Claude session flags
	if agentType == AgentClaude && opts.ClaudeSessionID != "" {
		if opts.Resume {
			agentCommand += " --resume " + shellQuote(opts.ClaudeSessionID)
		} else {
			agentCommand += " --session-id " + shellQuote(opts.ClaudeSessionID)
		}
	}

	// Skip permissions for group workspaces
	var sbplCleanup func()
	if gw.SkipPermissions && agentType == AgentClaude {
		agentCommand += " --dangerously-skip-permissions"
		_ = config.InjectSkipPermissionPrompt(profileDir)
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	fullCommand := fmt.Sprintf("%s; stty sane; printf '\\033[?1049l\\033[?25h\\033[0m\\033c'; echo 'Agent exited. Dropping to shell...'; export TERM=xterm-256color; exec %s -l", agentCommand, shell)

	if gw.Isolated {
		var gitDirs []string
		for _, sec := range gw.Secondary {
			if gd, err := git.ResolveWorktreeGitDir(sec.Root); err == nil {
				gitDirs = append(gitDirs, gd)
			}
		}
		sbpl := sandbox.GenerateSBPL(gw.Primary.Root, gitDirs, profileDir)
		sbplPath, cleanup, sErr := sandbox.WriteTempProfile(sbpl)
		if sErr == nil {
			sbplCleanup = cleanup
			fullCommand = sandbox.WrapCommand(fullCommand, sbplPath)
		}
	}

	termCommand := tmux.ClientCommandWithTags(sessionName, gw.Primary.Root, fullCommand, tmux.DefaultOptions(), tags)
	term, err := NewWithSize(termCommand, gw.Primary.Root, env, rows, cols)
	if err != nil {
		if sbplCleanup != nil {
			sbplCleanup()
		}
		return nil, fmt.Errorf("failed to create terminal: %w", err)
	}

	agent := &Agent{
		Type:      agentType,
		Terminal:  term,
		Workspace: &gw.Primary,
		Config:    assistantCfg,
		Session:   sessionName,
	}

	m.mu.Lock()
	m.agents[gw.ID()] = append(m.agents[gw.ID()], agent)
	m.mu.Unlock()

	return agent, nil
}

// CreateViewer creates a new agent (viewer) for the given workspace and command.
func (m *AgentManager) CreateViewer(ws *data.Workspace, command string, sessionName string, rows, cols uint16) (*Agent, error) {
	return m.CreateViewerWithTags(ws, command, sessionName, rows, cols, tmux.SessionTags{})
}

// CreateViewerWithTags creates a new viewer for the given workspace with tmux tags.
func (m *AgentManager) CreateViewerWithTags(ws *data.Workspace, command string, sessionName string, rows, cols uint16, tags tmux.SessionTags) (*Agent, error) {
	if ws == nil {
		return nil, fmt.Errorf("workspace is required")
	}
	if sessionName == "" {
		sessionName, _ = tmux.NextUniqueSessionName(ws.Name, tmux.DefaultOptions())
	}
	if err := tmux.EnsureAvailable(); err != nil {
		return nil, err
	}
	// Build environment
	env := []string{
		fmt.Sprintf("WORKSPACE_ROOT=%s", ws.Root),
		fmt.Sprintf("WORKSPACE_NAME=%s", ws.Name),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	}

	termCommand := tmux.ClientCommandWithTags(sessionName, ws.Root, command, tmux.DefaultOptions(), tags)
	term, err := NewWithSize(termCommand, ws.Root, env, rows, cols)
	if err != nil {
		return nil, fmt.Errorf("failed to create terminal: %w", err)
	}

	agent := &Agent{
		Type:      AgentType("viewer"),
		Terminal:  term,
		Workspace: ws,
		Config:    config.AssistantConfig{}, // No specific config
		Session:   sessionName,
	}

	m.mu.Lock()
	m.agents[ws.ID()] = append(m.agents[ws.ID()], agent)
	m.mu.Unlock()

	return agent, nil
}

// CloseAgent closes an agent
func (m *AgentManager) CloseAgent(agent *Agent) error {
	if agent.Terminal != nil {
		agent.Terminal.Close()
	}

	// Remove from list
	if agent.Workspace != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		agents := m.agents[agent.Workspace.ID()]
		for i, a := range agents {
			if a == agent {
				m.agents[agent.Workspace.ID()] = append(agents[:i], agents[i+1:]...)
				break
			}
		}
	}

	return nil
}

// CloseAll closes all agents
func (m *AgentManager) CloseAll() {
	m.mu.Lock()
	agentsByWorkspace := m.agents
	m.agents = make(map[data.WorkspaceID][]*Agent)
	m.mu.Unlock()

	for _, agents := range agentsByWorkspace {
		for _, agent := range agents {
			if agent.Terminal != nil {
				agent.Terminal.Close()
			}
		}
	}
}

// MigrateWorkspaceAgents moves agent state from oldID to newID after a workspace rename.
// It updates the workspace pointer and tmux session names on each agent without closing terminals.
// oldName/newName are workspace display names used to compute tmux session name prefixes.
func (m *AgentManager) MigrateWorkspaceAgents(oldID, newID data.WorkspaceID, ws *data.Workspace, oldName, newName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	oldPrefix := tmux.SessionName("medusa", oldName) + "-"
	newPrefix := tmux.SessionName("medusa", newName) + "-"
	if agents, ok := m.agents[oldID]; ok {
		for _, agent := range agents {
			agent.Workspace = ws
			if strings.HasPrefix(agent.Session, oldPrefix) {
				agent.Session = newPrefix + strings.TrimPrefix(agent.Session, oldPrefix)
			}
		}
		m.agents[newID] = agents
		delete(m.agents, oldID)
	}
}

// CloseWorkspaceAgents closes and removes all agents for a specific workspace
func (m *AgentManager) CloseWorkspaceAgents(ws *data.Workspace) {
	if ws == nil {
		return
	}
	wsID := ws.ID()
	m.mu.Lock()
	agents := m.agents[wsID]
	delete(m.agents, wsID)
	m.mu.Unlock()
	for _, agent := range agents {
		if agent.Terminal != nil {
			agent.Terminal.Close()
		}
	}
}

// shellQuote wraps a value in single quotes for safe shell embedding.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// SendInterrupt sends an interrupt to an agent
func (m *AgentManager) SendInterrupt(agent *Agent) error {
	if agent.Terminal == nil {
		return nil
	}

	// Send multiple interrupts if configured (e.g., for Claude)
	for i := 0; i < agent.Config.InterruptCount; i++ {
		if err := agent.Terminal.SendInterrupt(); err != nil {
			return err
		}
		// Add delay between interrupts if configured
		if i < agent.Config.InterruptCount-1 && agent.Config.InterruptDelayMs > 0 {
			time.Sleep(time.Duration(agent.Config.InterruptDelayMs) * time.Millisecond)
		}
	}

	return nil
}
