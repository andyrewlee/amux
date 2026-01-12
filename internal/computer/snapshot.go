package computer

import (
	"fmt"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/daytona"
)

const DefaultSnapshotBaseImage = "node:20-bullseye"

var DefaultSnapshotAgents = []Agent{
	AgentClaude,
	AgentCodex,
	AgentOpenCode,
	AgentAmp,
	AgentGemini,
	AgentDroid,
}

var agentInstalls = map[Agent][]string{
	AgentClaude: {
		// Native installer (recommended by Anthropic) - installs to ~/.local/bin/claude
		"curl -fsSL https://claude.ai/install.sh | bash",
		// Symlink to /usr/local/bin for reliable PATH resolution in all shells
		"ln -sf /root/.local/bin/claude /usr/local/bin/claude || true",
	},
	AgentCodex:    {"npm install -g @openai/codex"},
	AgentOpenCode: {"npm install -g opencode-ai"},
	AgentAmp: {
		"curl -fsSL https://ampcode.com/install.sh | bash",
		"ln -sf /root/.amp/bin/amp /usr/local/bin/amp || true",
	},
	AgentGemini: {"npm install -g @google/gemini-cli"},
	AgentDroid: {
		"curl -fsSL https://app.factory.ai/cli | sh",
		"ln -sf /root/.local/bin/droid /usr/local/bin/droid || true",
	},
	AgentShell: {},
}

// ParseAgentList parses a comma-separated list of agents.
func ParseAgentList(value string) ([]Agent, error) {
	if strings.TrimSpace(value) == "" {
		return append([]Agent{}, DefaultSnapshotAgents...), nil
	}
	items := strings.Split(value, ",")
	agents := make([]Agent, 0, len(items))
	seen := map[Agent]bool{}
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		agent := Agent(trimmed)
		switch agent {
		case AgentClaude, AgentCodex, AgentOpenCode, AgentAmp, AgentGemini, AgentDroid:
			if !seen[agent] {
				agents = append(agents, agent)
				seen[agent] = true
			}
		default:
			return nil, fmt.Errorf("unknown agent %q. Use: claude,codex,opencode,amp,gemini,droid", trimmed)
		}
	}
	if len(agents) == 0 {
		return append([]Agent{}, DefaultSnapshotAgents...), nil
	}
	return agents, nil
}

// BuildSnapshotName returns a timestamped snapshot name.
func BuildSnapshotName(prefix string) string {
	stamp := time.Now().UTC().Format("20060102-150405")
	return fmt.Sprintf("%s-%s", prefix, stamp)
}

// BuildSnapshotImage builds a Dockerfile image with agent installs.
func BuildSnapshotImage(agents []Agent, baseImage string) *daytona.Image {
	if baseImage == "" {
		baseImage = DefaultSnapshotBaseImage
	}
	image := daytona.ImageBase(baseImage)
	installCommands := []string{
		"apt-get update -y && apt-get install -y --no-install-recommends curl git ca-certificates && rm -rf /var/lib/apt/lists/*",
	}
	for _, agent := range agents {
		installCommands = append(installCommands, agentInstalls[agent]...)
	}
	for _, cmd := range installCommands {
		image.RunCommands([]string{"bash", "-lc", cmd})
	}
	return image
}

// CreateSnapshot builds and creates a snapshot.
func CreateSnapshot(client *daytona.Daytona, name string, agents []Agent, baseImage string, onLogs func(string)) (*daytona.Snapshot, error) {
	image := BuildSnapshotImage(agents, baseImage)
	return client.Snapshot.Create(daytona.CreateSnapshotParams{
		Name:  name,
		Image: image,
	}, &daytona.SnapshotCreateOptions{OnLogs: onLogs})
}
