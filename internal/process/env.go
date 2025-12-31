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

// BuildEnv creates environment variables for a worktree
func (b *EnvBuilder) BuildEnv(wt *data.Worktree, meta *data.Metadata) []string {
	env := os.Environ()

	// Add worktree-specific variables
	env = append(env,
		fmt.Sprintf("AMUX_WORKTREE_NAME=%s", wt.Name),
		fmt.Sprintf("AMUX_WORKTREE_ROOT=%s", wt.Root),
		fmt.Sprintf("AMUX_WORKTREE_BRANCH=%s", wt.Branch),
		fmt.Sprintf("AMUX_REPO_ROOT=%s", wt.Repo),
	)

	// Add port allocation
	if b.portAllocator != nil {
		port, rangeEnd := b.portAllocator.PortRange(wt.Root)
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
func (b *EnvBuilder) BuildEnvMap(wt *data.Worktree, meta *data.Metadata) map[string]string {
	envMap := make(map[string]string)

	envMap["AMUX_WORKTREE_NAME"] = wt.Name
	envMap["AMUX_WORKTREE_ROOT"] = wt.Root
	envMap["AMUX_WORKTREE_BRANCH"] = wt.Branch
	envMap["AMUX_REPO_ROOT"] = wt.Repo

	if b.portAllocator != nil {
		port, rangeEnd := b.portAllocator.PortRange(wt.Root)
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
