package process

import (
	"github.com/andyrewlee/amux/internal/data"
)

// SetProjectEnvResolver installs the lookup for the user-level per-project
// env map (secrets and overrides that live above repo `env` and beneath
// ws.Env). Called once at app init; nil means no project layer. The resolver
// is read on every spawn so edits take effect for existing workspaces —
// unlike ws.Env, the project map is intentionally not a create-time snapshot.
func (r *ScriptRunner) SetProjectEnvResolver(resolve func(repoPath string) map[string]string) {
	r.mu.Lock()
	r.projectEnv = resolve
	r.mu.Unlock()
}

// SetProjectScriptResolver installs the lookup for the user-level
// per-project script defaults (the layer beneath ws.Scripts). Called once at
// app init; nil means no project layer. The resolver is read on every
// resolution so a hand-edited project-scripts.json takes effect for existing
// workspaces without a restart — the same read-on-demand contract the env
// resolver gives project-env.json.
func (r *ScriptRunner) SetProjectScriptResolver(resolve func(repoPath string) data.ScriptsConfig) {
	r.mu.Lock()
	r.projectScripts = resolve
	r.mu.Unlock()
}

// projectScriptFor returns the project layer's command for scriptType, or ""
// when the layer is unwired, the project has none, or the file is unreadable
// (ForRepo's documented empty-on-error contract applies).
func (r *ScriptRunner) projectScriptFor(repoPath string, scriptType ScriptType) string {
	r.mu.Lock()
	resolve := r.projectScripts
	r.mu.Unlock()
	if resolve == nil {
		return ""
	}
	cfg := resolve(repoPath)
	switch scriptType {
	case ScriptSetup:
		return cfg.Setup
	case ScriptRun:
		return cfg.Run
	case ScriptArchive:
		return cfg.Archive
	case ScriptOnDone:
		return cfg.OnDone
	}
	return ""
}

// buildScriptEnv assembles the env for one script spawn with the documented
// precedence:
//
//	os.Environ < AMUX_* injected < repo `env` (trust-gated) < project env < ws.Env
//
// The repo layer is gated identically to repo scripts: a non-empty `env` in
// .amux/workspaces.json whose content is not yet approved returns
// *ScriptsNotTrustedError — the same fail-closed outcome as an untrusted
// `run`, so every script path surfaces the trust dialog instead of letting
// repo-chosen variables (PATH, LD_PRELOAD, GOFLAGS…) reach a spawned process.
func (r *ScriptRunner) buildScriptEnv(ws *data.Workspace) ([]string, error) {
	if err := validateScriptWorkspace(ws); err != nil {
		return nil, err
	}
	config, raw, err := r.loadConfigRaw(ws.Repo)
	if err != nil {
		return nil, err
	}
	var repoEnv map[string]string
	if len(config.Env) > 0 {
		if !r.trust.IsTrusted(ws.Repo, raw) {
			return nil, &ScriptsNotTrustedError{
				Repo:       ws.Repo,
				Command:    "env",
				ConfigHash: hashConfig(raw),
			}
		}
		repoEnv = config.Env
	}

	r.mu.Lock()
	resolve := r.projectEnv
	r.mu.Unlock()
	var projectEnv map[string]string
	if resolve != nil {
		projectEnv = resolve(ws.Repo)
	}

	return r.envBuilder.BuildEnvLayers(ws, repoEnv, projectEnv)
}
