package process

import (
	"fmt"
	"os"

	"github.com/andyrewlee/amux/internal/data"
)

// EnvBuilder builds environment variables for script execution
type EnvBuilder struct {
	portAllocator *PortAllocator
}

// NewEnvBuilder creates a new environment builder
func NewEnvBuilder(ports *PortAllocator) *EnvBuilder {
	return &EnvBuilder{
		portAllocator: ports,
	}
}

// BuildEnv creates environment variables for a workspace
func (b *EnvBuilder) BuildEnv(ws *data.Workspace, meta *data.Metadata) []string {
	env := os.Environ()

	// Add workspace-specific variables
	env = append(env,
		fmt.Sprintf("AMUX_WORKSPACE_NAME=%s", ws.Name),
		fmt.Sprintf("AMUX_WORKSPACE_ROOT=%s", ws.Root),
		fmt.Sprintf("AMUX_WORKSPACE_BRANCH=%s", ws.Branch),
		fmt.Sprintf("ROOT_WORKSPACE_PATH=%s", ws.Repo),
	)

	// Add port allocation
	if b.portAllocator != nil {
		port, rangeEnd := b.portAllocator.PortRange(ws.Root)
		env = append(env,
			fmt.Sprintf("AMUX_PORT=%d", port),
			fmt.Sprintf("AMUX_PORT_RANGE=%d-%d", port, rangeEnd),
		)
	}

	// Add custom environment from metadata
	if meta != nil {
		for k, v := range meta.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	return env
}

// BuildEnvMap creates a map of environment variables
func (b *EnvBuilder) BuildEnvMap(ws *data.Workspace, meta *data.Metadata) map[string]string {
	envMap := make(map[string]string)

	envMap["AMUX_WORKSPACE_NAME"] = ws.Name
	envMap["AMUX_WORKSPACE_ROOT"] = ws.Root
	envMap["AMUX_WORKSPACE_BRANCH"] = ws.Branch
	envMap["ROOT_WORKSPACE_PATH"] = ws.Repo

	if b.portAllocator != nil {
		port, rangeEnd := b.portAllocator.PortRange(ws.Root)
		envMap["AMUX_PORT"] = fmt.Sprintf("%d", port)
		envMap["AMUX_PORT_RANGE"] = fmt.Sprintf("%d-%d", port, rangeEnd)
	}

	if meta != nil {
		for k, v := range meta.Env {
			envMap[k] = v
		}
	}

	return envMap
}
