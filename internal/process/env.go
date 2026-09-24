package process

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

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
func (b *EnvBuilder) BuildEnv(ws *data.Workspace) ([]string, error) {
	return b.BuildEnvLayers(ws)
}

// BuildEnvLayers is BuildEnv with additional env maps layered between the
// injected AMUX_* names and ws.Env. Layers apply in argument order and each
// later layer wins on key conflict — Go's exec keeps the last duplicate
// assignment — so the documented precedence is:
//
//	os.Environ < AMUX_* injected < layers (in order) < ws.Env
//
// The caller's intended order is repo `env` (trust-gated) then project-level
// user env, so a per-project override can correct a repo default while the
// workspace map remains the final word. Reserved AMUX_*/ROOT_* keys are
// filtered from every layer just as they are from ws.Env.
func (b *EnvBuilder) BuildEnvLayers(ws *data.Workspace, layers ...map[string]string) ([]string, error) {
	env := os.Environ()
	if ws == nil {
		return env, nil
	}

	// Add workspace-specific variables
	env = append(env,
		"AMUX_WORKSPACE_NAME="+ws.Name,
		"AMUX_WORKSPACE_ROOT="+ws.Root,
		"AMUX_WORKSPACE_BRANCH="+ws.Branch,
		"ROOT_WORKSPACE_PATH="+ws.Repo,
	)

	// Add port allocation
	if b != nil && b.portAllocator != nil {
		port, rangeEnd, err := b.portAllocator.PortRange(ws.Root)
		if err != nil {
			return nil, err
		}
		env = append(env,
			fmt.Sprintf("AMUX_PORT=%d", port),
			fmt.Sprintf("AMUX_PORT_RANGE=%d-%d", port, rangeEnd),
		)
	}

	for _, layer := range layers {
		env = appendLayerEnv(env, layer)
	}
	env = appendLayerEnv(env, ws.Env)

	return env, nil
}

// appendLayerEnv appends one layer's filtered keys in sorted order (the same
// deterministic rendering BuildEnv used for ws.Env).
func appendLayerEnv(env []string, layer map[string]string) []string {
	keys := make([]string, 0, len(layer))
	for k := range layer {
		if !isReservedScriptEnvKey(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, fmt.Sprintf("%s=%s", k, layer[k]))
	}
	return env
}

// BuildEnvMap creates a map of environment variables
func (b *EnvBuilder) BuildEnvMap(ws *data.Workspace) (map[string]string, error) {
	envMap := make(map[string]string)
	if ws == nil {
		return envMap, nil
	}

	envMap["AMUX_WORKSPACE_NAME"] = ws.Name
	envMap["AMUX_WORKSPACE_ROOT"] = ws.Root
	envMap["AMUX_WORKSPACE_BRANCH"] = ws.Branch
	envMap["ROOT_WORKSPACE_PATH"] = ws.Repo

	if b != nil && b.portAllocator != nil {
		port, rangeEnd, err := b.portAllocator.PortRange(ws.Root)
		if err != nil {
			return nil, err
		}
		envMap["AMUX_PORT"] = strconv.Itoa(port)
		envMap["AMUX_PORT_RANGE"] = fmt.Sprintf("%d-%d", port, rangeEnd)
	}

	for k, v := range ws.Env {
		if isReservedScriptEnvKey(k) {
			continue
		}
		envMap[k] = v
	}

	return envMap, nil
}

// IsReservedScriptEnvKey reports whether key is in the reserved AMUX_*/ROOT_*
// namespace (see isReservedScriptEnvKey below). It is exported so callers
// outside this package -- namely the environment-variable editors and the
// trust review surface -- can exclude reserved keys before they are shown or
// persisted, without duplicating or drifting from the policy enforced here.
func IsReservedScriptEnvKey(key string) bool {
	return isReservedScriptEnvKey(key)
}

// isReservedScriptEnvKey guards the AMUX_*/ROOT_* namespaces: amux injects
// names under both (AMUX_WORKSPACE_*, AMUX_PORT*, ROOT_WORKSPACE_PATH, plus
// per-spawn names like AMUX_SESSION and AMUX_REMOVE_PATH), and CONFIG.md
// promises they can never be overridden by custom env layers. The filter is
// prefix-based, not a list, so every future injected name is protected
// automatically — user scripts that want custom vars simply use any other
// prefix. The user's own os.Environ is trusted and never passes through this.
func isReservedScriptEnvKey(key string) bool {
	return strings.HasPrefix(key, "AMUX_") || strings.HasPrefix(key, "ROOT_")
}
