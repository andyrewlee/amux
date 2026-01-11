package sandbox

import (
	"fmt"
	"strings"

	"github.com/andyrewlee/amux/internal/daytona"
)

// AgentPlugin defines the interface for agent implementations.
// This allows adding new agents without modifying core code.
type AgentPlugin interface {
	// Name returns the agent's identifier (e.g., "claude", "codex").
	Name() string

	// DisplayName returns a human-friendly name (e.g., "Claude Code").
	DisplayName() string

	// Description returns a short description of the agent.
	Description() string

	// InstallMethods returns the installation methods in priority order.
	InstallMethods() []InstallMethod

	// CredentialPaths returns paths where credentials are stored.
	// Paths are relative to the credentials volume mount.
	CredentialPaths() []CredentialPath

	// SettingsPaths returns paths where settings are stored locally.
	// Paths are relative to the user's home directory.
	SettingsPaths() []SettingsPath

	// ContextFiles returns project-level context files the agent reads.
	// (e.g., CLAUDE.md, AGENT.md)
	ContextFiles() []string

	// EnvVars returns environment variables that can configure the agent.
	EnvVars() []EnvVarSpec

	// LoginCommands returns the commands to run for authentication.
	// Empty if the agent doesn't require explicit login.
	LoginCommands() []string

	// VersionCommand returns the command to check the installed version.
	VersionCommand() string

	// Validate checks if the agent is properly installed and configured.
	Validate(sandbox *daytona.Sandbox) error
}

// InstallMethod describes how to install an agent.
type InstallMethod struct {
	Type    InstallType // npm, curl, binary, etc.
	Command string      // The installation command
	Package string      // Package name (for npm/pip)
	URL     string      // URL (for curl installers)
}

type InstallType string

const (
	InstallTypeNPM    InstallType = "npm"
	InstallTypeCurl   InstallType = "curl"
	InstallTypeBinary InstallType = "binary"
	InstallTypePip    InstallType = "pip"
	InstallTypeGo     InstallType = "go"
)

// CredentialPath describes where an agent stores credentials.
type CredentialPath struct {
	VolumePath string // Path in the credentials volume (e.g., "claude/.credentials.json")
	HomePath   string // Path in home directory to symlink (e.g., ".claude")
	IsDir      bool   // Whether this is a directory or file
}

// SettingsPath describes where an agent stores settings locally.
type SettingsPath struct {
	LocalPath   string   // Path relative to home (e.g., ".claude/settings.json")
	VolumePath  string   // Path in volume to sync to
	Description string   // Human-readable description
	SafeKeys    []string // If JSON, only sync these keys (empty = all safe keys)
}

// EnvVarSpec describes an environment variable an agent uses.
type EnvVarSpec struct {
	Name        string // Variable name (e.g., "ANTHROPIC_API_KEY")
	Description string // What it's used for
	Required    bool   // Whether it's required
	Secret      bool   // Whether it contains sensitive data
}

// AgentRegistry manages registered agent plugins.
type AgentRegistry struct {
	plugins map[string]AgentPlugin
}

// NewAgentRegistry creates a new agent registry.
func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		plugins: make(map[string]AgentPlugin),
	}
}

// Register adds a plugin to the registry.
func (r *AgentRegistry) Register(plugin AgentPlugin) {
	r.plugins[plugin.Name()] = plugin
}

// Get returns a plugin by name.
func (r *AgentRegistry) Get(name string) (AgentPlugin, bool) {
	plugin, ok := r.plugins[name]
	return plugin, ok
}

// All returns all registered plugins.
func (r *AgentRegistry) All() []AgentPlugin {
	plugins := make([]AgentPlugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		plugins = append(plugins, p)
	}
	return plugins
}

// Names returns all registered plugin names.
func (r *AgentRegistry) Names() []string {
	names := make([]string, 0, len(r.plugins))
	for name := range r.plugins {
		names = append(names, name)
	}
	return names
}

// Global agent registry
var defaultRegistry = NewAgentRegistry()

// RegisterAgent registers an agent plugin globally.
func RegisterAgent(plugin AgentPlugin) {
	defaultRegistry.Register(plugin)
}

// GetAgentPlugin returns a registered agent plugin.
func GetAgentPlugin(name string) (AgentPlugin, bool) {
	return defaultRegistry.Get(name)
}

// AllAgentPlugins returns all registered agent plugins.
func AllAgentPlugins() []AgentPlugin {
	return defaultRegistry.All()
}

// ========== Built-in Agent Implementations ==========

// ClaudePlugin implements AgentPlugin for Claude Code.
type ClaudePlugin struct{}

func (p *ClaudePlugin) Name() string        { return "claude" }
func (p *ClaudePlugin) DisplayName() string { return "Claude Code" }
func (p *ClaudePlugin) Description() string {
	return "Anthropic's AI coding assistant"
}

func (p *ClaudePlugin) InstallMethods() []InstallMethod {
	return []InstallMethod{
		{Type: InstallTypeNPM, Package: "@anthropic-ai/claude-code@latest"},
	}
}

func (p *ClaudePlugin) CredentialPaths() []CredentialPath {
	return []CredentialPath{
		{VolumePath: "claude", HomePath: ".claude", IsDir: true},
	}
}

func (p *ClaudePlugin) SettingsPaths() []SettingsPath {
	return []SettingsPath{
		{
			LocalPath:   ".claude/settings.json",
			VolumePath:  "claude/settings.json",
			Description: "Claude Code settings (model preferences, features, permissions)",
		},
	}
}

func (p *ClaudePlugin) ContextFiles() []string {
	return []string{"CLAUDE.md", ".claude/settings.local.json"}
}

func (p *ClaudePlugin) EnvVars() []EnvVarSpec {
	return []EnvVarSpec{
		{Name: "ANTHROPIC_API_KEY", Description: "Anthropic API key", Secret: true},
		{Name: "CLAUDE_API_KEY", Description: "Alternative API key name", Secret: true},
		{Name: "ANTHROPIC_AUTH_TOKEN", Description: "OAuth token", Secret: true},
	}
}

func (p *ClaudePlugin) LoginCommands() []string {
	return nil // Claude handles login interactively
}

func (p *ClaudePlugin) VersionCommand() string {
	return "claude --version"
}

func (p *ClaudePlugin) Validate(sandbox *daytona.Sandbox) error {
	resp, err := sandbox.Process.ExecuteCommand("command -v claude")
	if err != nil || resp.ExitCode != 0 {
		return fmt.Errorf("claude not found in PATH")
	}
	return nil
}

// CodexPlugin implements AgentPlugin for OpenAI Codex.
type CodexPlugin struct{}

func (p *CodexPlugin) Name() string        { return "codex" }
func (p *CodexPlugin) DisplayName() string { return "Codex CLI" }
func (p *CodexPlugin) Description() string {
	return "OpenAI's Codex coding agent"
}

func (p *CodexPlugin) InstallMethods() []InstallMethod {
	return []InstallMethod{
		{Type: InstallTypeNPM, Package: "@openai/codex@latest"},
	}
}

func (p *CodexPlugin) CredentialPaths() []CredentialPath {
	return []CredentialPath{
		{VolumePath: "codex", HomePath: ".codex", IsDir: true},
		{VolumePath: "codex", HomePath: ".config/codex", IsDir: true},
	}
}

func (p *CodexPlugin) SettingsPaths() []SettingsPath {
	return []SettingsPath{
		{
			LocalPath:   ".codex/config.toml",
			VolumePath:  "codex/config.toml",
			Description: "Codex CLI settings (model preferences, editor config)",
		},
	}
}

func (p *CodexPlugin) ContextFiles() []string {
	return []string{"AGENTS.md", "codex.md"}
}

func (p *CodexPlugin) EnvVars() []EnvVarSpec {
	return []EnvVarSpec{
		{Name: "OPENAI_API_KEY", Description: "OpenAI API key", Secret: true},
	}
}

func (p *CodexPlugin) LoginCommands() []string {
	return []string{"login", "--device-auth"}
}

func (p *CodexPlugin) VersionCommand() string {
	return "codex --version"
}

func (p *CodexPlugin) Validate(sandbox *daytona.Sandbox) error {
	resp, err := sandbox.Process.ExecuteCommand("command -v codex")
	if err != nil || resp.ExitCode != 0 {
		return fmt.Errorf("codex not found in PATH")
	}
	return nil
}

// OpenCodePlugin implements AgentPlugin for OpenCode.
type OpenCodePlugin struct{}

func (p *OpenCodePlugin) Name() string        { return "opencode" }
func (p *OpenCodePlugin) DisplayName() string { return "OpenCode" }
func (p *OpenCodePlugin) Description() string {
	return "Open source AI coding agent"
}

func (p *OpenCodePlugin) InstallMethods() []InstallMethod {
	return []InstallMethod{
		{Type: InstallTypeCurl, URL: "https://opencode.ai/install"},
		{Type: InstallTypeNPM, Package: "opencode-ai@latest"},
	}
}

func (p *OpenCodePlugin) CredentialPaths() []CredentialPath {
	return []CredentialPath{
		{VolumePath: "opencode", HomePath: ".local/share/opencode", IsDir: true},
		{VolumePath: "opencode", HomePath: ".config/opencode", IsDir: true},
	}
}

func (p *OpenCodePlugin) SettingsPaths() []SettingsPath {
	return []SettingsPath{
		{
			LocalPath:   ".config/opencode/config.json",
			VolumePath:  "opencode/config.json",
			Description: "OpenCode settings (model preferences, keybindings)",
		},
	}
}

func (p *OpenCodePlugin) ContextFiles() []string {
	return []string{}
}

func (p *OpenCodePlugin) EnvVars() []EnvVarSpec {
	return []EnvVarSpec{
		{Name: "ANTHROPIC_API_KEY", Description: "Anthropic API key for Claude models", Secret: true},
		{Name: "OPENAI_API_KEY", Description: "OpenAI API key", Secret: true},
		{Name: "GEMINI_API_KEY", Description: "Google Gemini API key", Secret: true},
	}
}

func (p *OpenCodePlugin) LoginCommands() []string {
	return []string{"auth", "login"}
}

func (p *OpenCodePlugin) VersionCommand() string {
	return "opencode --version"
}

func (p *OpenCodePlugin) Validate(sandbox *daytona.Sandbox) error {
	resp, err := sandbox.Process.ExecuteCommand("command -v opencode")
	if err != nil || resp.ExitCode != 0 {
		return fmt.Errorf("opencode not found in PATH")
	}
	return nil
}

// AmpPlugin implements AgentPlugin for Sourcegraph Amp.
type AmpPlugin struct{}

func (p *AmpPlugin) Name() string        { return "amp" }
func (p *AmpPlugin) DisplayName() string { return "Amp" }
func (p *AmpPlugin) Description() string {
	return "Sourcegraph's AI coding agent"
}

func (p *AmpPlugin) InstallMethods() []InstallMethod {
	return []InstallMethod{
		{Type: InstallTypeCurl, URL: "https://ampcode.com/install.sh"},
		{Type: InstallTypeNPM, Package: "@sourcegraph/amp@latest"},
	}
}

func (p *AmpPlugin) CredentialPaths() []CredentialPath {
	return []CredentialPath{
		{VolumePath: "amp", HomePath: ".config/amp", IsDir: true},
		{VolumePath: "amp", HomePath: ".local/share/amp", IsDir: true},
	}
}

func (p *AmpPlugin) SettingsPaths() []SettingsPath {
	return []SettingsPath{
		{
			LocalPath:   ".config/amp/config.json",
			VolumePath:  "amp/config.json",
			Description: "Amp settings (model preferences, workspace config)",
		},
	}
}

func (p *AmpPlugin) ContextFiles() []string {
	return []string{"AGENT.md"}
}

func (p *AmpPlugin) EnvVars() []EnvVarSpec {
	return []EnvVarSpec{
		{Name: "AMP_API_KEY", Description: "Amp API key", Secret: true},
	}
}

func (p *AmpPlugin) LoginCommands() []string {
	return []string{"login"}
}

func (p *AmpPlugin) VersionCommand() string {
	return "amp --version"
}

func (p *AmpPlugin) Validate(sandbox *daytona.Sandbox) error {
	resp, err := sandbox.Process.ExecuteCommand("command -v amp || test -x $HOME/.amp/bin/amp")
	if err != nil || resp.ExitCode != 0 {
		return fmt.Errorf("amp not found in PATH")
	}
	return nil
}

// GeminiPlugin implements AgentPlugin for Google Gemini CLI.
type GeminiPlugin struct{}

func (p *GeminiPlugin) Name() string        { return "gemini" }
func (p *GeminiPlugin) DisplayName() string { return "Gemini CLI" }
func (p *GeminiPlugin) Description() string {
	return "Google's Gemini AI coding agent"
}

func (p *GeminiPlugin) InstallMethods() []InstallMethod {
	return []InstallMethod{
		{Type: InstallTypeNPM, Package: "@google/gemini-cli@latest"},
	}
}

func (p *GeminiPlugin) CredentialPaths() []CredentialPath {
	return []CredentialPath{
		{VolumePath: "gemini", HomePath: ".gemini", IsDir: true},
	}
}

func (p *GeminiPlugin) SettingsPaths() []SettingsPath {
	return []SettingsPath{
		{
			LocalPath:   ".gemini/settings.json",
			VolumePath:  "gemini/settings.json",
			Description: "Gemini CLI settings (model preferences)",
		},
	}
}

func (p *GeminiPlugin) ContextFiles() []string {
	return []string{"GEMINI.md"}
}

func (p *GeminiPlugin) EnvVars() []EnvVarSpec {
	return []EnvVarSpec{
		{Name: "GEMINI_API_KEY", Description: "Gemini API key", Secret: true},
		{Name: "GOOGLE_API_KEY", Description: "Google API key", Secret: true},
		{Name: "GOOGLE_APPLICATION_CREDENTIALS", Description: "Service account credentials", Secret: true},
	}
}

func (p *GeminiPlugin) LoginCommands() []string {
	return nil // Gemini handles login interactively
}

func (p *GeminiPlugin) VersionCommand() string {
	return "gemini --version"
}

func (p *GeminiPlugin) Validate(sandbox *daytona.Sandbox) error {
	resp, err := sandbox.Process.ExecuteCommand("command -v gemini")
	if err != nil || resp.ExitCode != 0 {
		return fmt.Errorf("gemini not found in PATH")
	}
	return nil
}

// DroidPlugin implements AgentPlugin for Factory Droid.
type DroidPlugin struct{}

func (p *DroidPlugin) Name() string        { return "droid" }
func (p *DroidPlugin) DisplayName() string { return "Droid" }
func (p *DroidPlugin) Description() string {
	return "Factory's AI coding agent"
}

func (p *DroidPlugin) InstallMethods() []InstallMethod {
	return []InstallMethod{
		{Type: InstallTypeCurl, URL: "https://app.factory.ai/cli"},
	}
}

func (p *DroidPlugin) CredentialPaths() []CredentialPath {
	return []CredentialPath{
		{VolumePath: "factory", HomePath: ".factory", IsDir: true},
	}
}

func (p *DroidPlugin) SettingsPaths() []SettingsPath {
	return []SettingsPath{}
}

func (p *DroidPlugin) ContextFiles() []string {
	return []string{}
}

func (p *DroidPlugin) EnvVars() []EnvVarSpec {
	return []EnvVarSpec{
		{Name: "FACTORY_API_KEY", Description: "Factory API key", Secret: true},
	}
}

func (p *DroidPlugin) LoginCommands() []string {
	return nil // Droid handles login via /login command
}

func (p *DroidPlugin) VersionCommand() string {
	return "droid --version"
}

func (p *DroidPlugin) Validate(sandbox *daytona.Sandbox) error {
	resp, err := sandbox.Process.ExecuteCommand("command -v droid || test -x $HOME/.factory/bin/droid")
	if err != nil || resp.ExitCode != 0 {
		return fmt.Errorf("droid not found in PATH")
	}
	return nil
}

// ShellPlugin implements AgentPlugin for a basic shell.
type ShellPlugin struct{}

func (p *ShellPlugin) Name() string                      { return "shell" }
func (p *ShellPlugin) DisplayName() string               { return "Shell" }
func (p *ShellPlugin) Description() string               { return "Interactive bash shell" }
func (p *ShellPlugin) InstallMethods() []InstallMethod   { return nil }
func (p *ShellPlugin) CredentialPaths() []CredentialPath { return nil }
func (p *ShellPlugin) SettingsPaths() []SettingsPath     { return nil }
func (p *ShellPlugin) ContextFiles() []string            { return nil }
func (p *ShellPlugin) EnvVars() []EnvVarSpec             { return nil }
func (p *ShellPlugin) LoginCommands() []string           { return nil }
func (p *ShellPlugin) VersionCommand() string            { return "bash --version" }
func (p *ShellPlugin) Validate(sandbox *daytona.Sandbox) error {
	return nil // bash is always available
}

// Initialize registers all built-in plugins.
func init() {
	RegisterAgent(&ClaudePlugin{})
	RegisterAgent(&CodexPlugin{})
	RegisterAgent(&OpenCodePlugin{})
	RegisterAgent(&AmpPlugin{})
	RegisterAgent(&GeminiPlugin{})
	RegisterAgent(&DroidPlugin{})
	RegisterAgent(&ShellPlugin{})
}

// Helper function to get install command for a plugin.
func GetInstallCommand(plugin AgentPlugin, sandbox *daytona.Sandbox) string {
	methods := plugin.InstallMethods()
	if len(methods) == 0 {
		return ""
	}

	// Try each method in order
	for _, method := range methods {
		switch method.Type {
		case InstallTypeNPM:
			return fmt.Sprintf("npm install -g %s", method.Package)
		case InstallTypeCurl:
			return fmt.Sprintf("curl -fsSL %s | bash", method.URL)
		case InstallTypePip:
			return fmt.Sprintf("pip install %s", method.Package)
		case InstallTypeGo:
			return fmt.Sprintf("go install %s", method.Package)
		}
	}
	return ""
}

// HasCredentials checks if the agent has credentials in the sandbox.
func HasCredentials(plugin AgentPlugin, sandbox *daytona.Sandbox) bool {
	for _, cred := range plugin.CredentialPaths() {
		checkPath := fmt.Sprintf("%s/%s", CredentialsMountPath, cred.VolumePath)
		var cmd string
		if cred.IsDir {
			cmd = fmt.Sprintf("test -d %s && ls -A %s | head -1", ShellQuote(checkPath), ShellQuote(checkPath))
		} else {
			cmd = SafeCommands.Test("-f", checkPath)
		}
		resp, err := sandbox.Process.ExecuteCommand(cmd)
		if err == nil && resp.ExitCode == 0 {
			stdout := strings.TrimSpace(getStdoutFromResp(resp))
			if !cred.IsDir || stdout != "" {
				return true
			}
		}
	}
	return false
}

func getStdoutFromResp(resp *daytona.ExecuteResponse) string {
	if resp == nil {
		return ""
	}
	if resp.Artifacts != nil && resp.Artifacts.Stdout != "" {
		return resp.Artifacts.Stdout
	}
	return resp.Result
}
