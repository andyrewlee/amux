package sandbox

// Agent identifies the CLI agents supported by AMUX sandboxes.
type Agent string

const (
	AgentClaude   Agent = "claude"
	AgentCodex    Agent = "codex"
	AgentOpenCode Agent = "opencode"
	AgentAmp      Agent = "amp"
	AgentGemini   Agent = "gemini"
	AgentDroid    Agent = "droid"
	AgentShell    Agent = "shell"
)

func (a Agent) String() string { return string(a) }

func IsValidAgent(value string) bool {
	switch value {
	case string(AgentClaude), string(AgentCodex), string(AgentOpenCode), string(AgentAmp), string(AgentGemini), string(AgentDroid), string(AgentShell):
		return true
	default:
		return false
	}
}

// VolumeSpec defines a named volume mount.
type VolumeSpec struct {
	Name      string
	MountPath string
}
