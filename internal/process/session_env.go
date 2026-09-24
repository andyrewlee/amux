package process

import (
	"github.com/andyrewlee/amux/internal/data"
)

// BuildSessionEnv assembles the env for an interactive session spawned in a
// workspace — an agent PTY or a sidebar terminal — the user-facing sibling
// of buildScriptEnv with one deliberate narrowing: repo `env` never applies.
//
//	os.Environ < AMUX_* injected < project env < ws.Env
//
// Trust-scope decision: interactive sessions get every user-controlled
// layer — the injected AMUX_* identity/port vars (sharing this runner's
// PortAllocator, so a session reports the workspace's real reservation),
// the user-owned project env map, and ws.Env — but NOT the repo layer. A
// .amux/workspaces.json env entry is repo-chosen content, trust-gated for
// scripts; an agent or shell is a long-lived interactive process acting
// with the user's credentials, so repo-chosen vars (PATH, *_BASE_URL,
// LD_PRELOAD) would carry a strictly larger blast radius than the one-shot
// script spawns the gate was designed for. Interactive sessions therefore
// never smuggle repo env in under a script trust approval — trusting a
// repo's scripts does not widen what the repo can inject.
func (r *ScriptRunner) BuildSessionEnv(ws *data.Workspace) ([]string, error) {
	r.mu.Lock()
	resolve := r.projectEnv
	r.mu.Unlock()
	var projectEnv map[string]string
	if resolve != nil {
		projectEnv = resolve(ws.Repo)
	}
	return r.envBuilder.BuildEnvLayers(ws, projectEnv)
}
