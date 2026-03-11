package sandbox

import (
	"errors"
	"fmt"
	"strings"
)

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
		{HomePath: ".claude", IsDir: true},
	}
}

func (p *ClaudePlugin) SettingsPaths() []SettingsPath {
	return []SettingsPath{
		{
			LocalPath:   ".claude/settings.json",
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

func (p *ClaudePlugin) Validate(computer RemoteSandbox) error {
	resp, err := execCommand(computer, "command -v claude", nil)
	if err != nil || resp.ExitCode != 0 {
		return errors.New("claude not found in PATH")
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
		{HomePath: ".codex", IsDir: true},
		{HomePath: ".config/codex", IsDir: true},
	}
}

func (p *CodexPlugin) SettingsPaths() []SettingsPath {
	return []SettingsPath{
		{
			LocalPath:   ".codex/config.toml",
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

func (p *CodexPlugin) Validate(computer RemoteSandbox) error {
	resp, err := execCommand(computer, "command -v codex", nil)
	if err != nil || resp.ExitCode != 0 {
		return errors.New("codex not found in PATH")
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
		{HomePath: ".local/share/opencode", IsDir: true},
		{HomePath: ".config/opencode", IsDir: true},
	}
}

func (p *OpenCodePlugin) SettingsPaths() []SettingsPath {
	return []SettingsPath{
		{
			LocalPath:   ".config/opencode/config.json",
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

func (p *OpenCodePlugin) Validate(computer RemoteSandbox) error {
	resp, err := execCommand(computer, "command -v opencode", nil)
	if err != nil || resp.ExitCode != 0 {
		return errors.New("opencode not found in PATH")
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
		{HomePath: ".config/amp", IsDir: true},
		{HomePath: ".local/share/amp", IsDir: true},
	}
}

func (p *AmpPlugin) SettingsPaths() []SettingsPath {
	return []SettingsPath{
		{
			LocalPath:   ".config/amp/config.json",
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

func (p *AmpPlugin) Validate(computer RemoteSandbox) error {
	resp, err := execCommand(computer, "command -v amp || test -x $HOME/.amp/bin/amp", nil)
	if err != nil || resp.ExitCode != 0 {
		return errors.New("amp not found in PATH")
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
		{HomePath: ".gemini", IsDir: true},
	}
}

func (p *GeminiPlugin) SettingsPaths() []SettingsPath {
	return []SettingsPath{
		{
			LocalPath:   ".gemini/settings.json",
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

func (p *GeminiPlugin) Validate(computer RemoteSandbox) error {
	resp, err := execCommand(computer, "command -v gemini", nil)
	if err != nil || resp.ExitCode != 0 {
		return errors.New("gemini not found in PATH")
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
		{HomePath: ".factory", IsDir: true},
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

func (p *DroidPlugin) Validate(computer RemoteSandbox) error {
	resp, err := execCommand(computer, "command -v droid || test -x $HOME/.factory/bin/droid", nil)
	if err != nil || resp.ExitCode != 0 {
		return errors.New("droid not found in PATH")
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
func (p *ShellPlugin) Validate(computer RemoteSandbox) error {
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

// HasCredentials checks if the agent has credentials in the sandbox.
func HasCredentials(plugin AgentPlugin, computer RemoteSandbox) bool {
	homeDir := getSandboxHomeDir(computer)
	for _, cred := range plugin.CredentialPaths() {
		checkPath := fmt.Sprintf("%s/%s", homeDir, cred.HomePath)
		var cmd string
		if cred.IsDir {
			cmd = fmt.Sprintf("test -d %s && ls -A %s | head -1", ShellQuote(checkPath), ShellQuote(checkPath))
		} else {
			cmd = SafeCommands.Test("-f", checkPath)
		}
		resp, err := execCommand(computer, cmd, nil)
		if err == nil && resp.ExitCode == 0 {
			stdout := strings.TrimSpace(getStdoutFromResp(resp))
			if !cred.IsDir || stdout != "" {
				return true
			}
		}
	}
	return false
}

func getStdoutFromResp(resp *ExecResult) string {
	if resp == nil {
		return ""
	}
	return resp.Stdout
}
