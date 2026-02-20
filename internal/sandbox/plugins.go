package sandbox

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
	// Paths are relative to the home directory.
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
	Validate(computer RemoteSandbox) error
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
	HomePath string // Path in home directory (e.g., ".claude")
	IsDir    bool   // Whether this is a directory or file
}

// SettingsPath describes where an agent stores settings locally.
type SettingsPath struct {
	LocalPath   string   // Path relative to home (e.g., ".claude/settings.json")
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
