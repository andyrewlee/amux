package process

import (
	"errors"
	"testing"
)

// TestBuildSessionEnv_IncludesInjectedAndUserLayers pins the interactive
// session contract: agents and sidebar terminals see the injected AMUX_*
// identity/port vars plus every user-controlled layer (project env, ws.Env).
func TestBuildSessionEnv_IncludesInjectedAndUserLayers(t *testing.T) {
	repo := t.TempDir()
	runner := NewScriptRunner(6200, 10)
	runner.SetProjectEnvResolver(func(string) map[string]string {
		return map[string]string{"PROJ_VAR": "pv", "SHARED": "project"}
	})
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo
	ws.Env = map[string]string{"WS_VAR": "wv", "SHARED": "ws"}

	env, err := runner.BuildSessionEnv(ws)
	if err != nil {
		t.Fatalf("BuildSessionEnv() error = %v", err)
	}
	m := envSliceToMap(env)
	if m["AMUX_WORKSPACE_ROOT"] != ws.Root || m["AMUX_WORKSPACE_NAME"] != ws.Name {
		t.Fatalf("injected identity vars missing: %v", m)
	}
	if m["AMUX_PORT"] == "" || m["AMUX_PORT_RANGE"] == "" {
		t.Fatalf("injected port vars missing: %v", m)
	}
	if m["PROJ_VAR"] != "pv" || m["WS_VAR"] != "wv" {
		t.Fatalf("project/ws env vars missing: %v", m)
	}
	if m["SHARED"] != "ws" {
		t.Fatalf("SHARED = %q, want ws (project < ws)", m["SHARED"])
	}
}

// TestBuildSessionEnv_RepoEnvNeverApplies is the trust-boundary half of the
// contract: even a *trusted* repo's env layer must not reach an interactive
// session. Repo env is repo-chosen content gated for one-shot scripts; a
// long-lived agent/shell acting with the user's credentials is a strictly
// larger blast radius, so the script trust grant does not widen it.
func TestBuildSessionEnv_RepoEnvNeverApplies(t *testing.T) {
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"env": {"REPO_VAR": "rv", "SHARED": "repo"}}`)
	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo) // trusted — must still not reach sessions
	runner.SetProjectEnvResolver(func(string) map[string]string {
		return map[string]string{"SHARED": "project"}
	})
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo

	env, err := runner.BuildSessionEnv(ws)
	if err != nil {
		t.Fatalf("BuildSessionEnv() error = %v", err)
	}
	m := envSliceToMap(env)
	if _, ok := m["REPO_VAR"]; ok {
		t.Fatalf("trusted repo env reached a session: %v", m)
	}
	if m["SHARED"] != "project" {
		t.Fatalf("SHARED = %q, want project (repo layer skipped entirely)", m["SHARED"])
	}
}

// TestBuildSessionEnv_UntrustedRepoDoesNotError proves the trust gate simply
// does not apply to sessions: a repo env layer with no trust grant neither
// errors (as buildScriptEnv would) nor leaks vars.
func TestBuildSessionEnv_UntrustedRepoDoesNotError(t *testing.T) {
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"env": {"REPO_VAR": "rv"}}`)
	runner := NewScriptRunner(6200, 10)
	useTempTrust(t, runner) // isolated, trust withheld
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo

	env, err := runner.BuildSessionEnv(ws)
	if err != nil {
		t.Fatalf("BuildSessionEnv() error = %v, want nil (gate is script-only)", err)
	}
	if _, ok := envSliceToMap(env)["REPO_VAR"]; ok {
		t.Fatal("untrusted repo env reached a session")
	}
}

// TestBuildSessionEnv_ReservedKeysFiltered proves user layers can't override
// injected runtime vars in sessions either.
func TestBuildSessionEnv_ReservedKeysFiltered(t *testing.T) {
	repo := t.TempDir()
	runner := NewScriptRunner(6200, 10)
	runner.SetProjectEnvResolver(func(string) map[string]string {
		return map[string]string{"AMUX_PORT": "1", "ORDINARY": "ok"}
	})
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo
	ws.Env = map[string]string{"ROOT_WORKSPACE_PATH": "/evil"}

	env, err := runner.BuildSessionEnv(ws)
	if err != nil {
		t.Fatalf("BuildSessionEnv() error = %v", err)
	}
	m := envSliceToMap(env)
	if m["AMUX_PORT"] == "1" || m["ROOT_WORKSPACE_PATH"] == "/evil" {
		t.Fatalf("reserved key overrode an injected value: %v", m)
	}
	if m["ORDINARY"] != "ok" {
		t.Fatal("ordinary project env key lost")
	}
}

// TestBuildSessionEnv_PortExhaustionReturnsError proves allocator exhaustion
// reaches session spawn paths as ErrPortRangeExhausted rather than silently
// producing a session without its reservation.
func TestBuildSessionEnv_PortExhaustionReturnsError(t *testing.T) {
	repo := t.TempDir()
	runner := NewScriptRunner(1, 32768) // range covers the entire port space
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo
	ws.Root = t.TempDir()

	other := *ws
	other.Root = t.TempDir()
	if _, err := runner.BuildSessionEnv(&other); err != nil {
		t.Fatalf("first BuildSessionEnv() error = %v", err)
	}
	if _, err := runner.BuildSessionEnv(ws); !errors.Is(err, ErrPortRangeExhausted) {
		t.Fatalf("exhausted BuildSessionEnv() error = %v, want ErrPortRangeExhausted", err)
	}
}

// TestBuildSessionEnv_SharesScriptPortReservation proves the session env
// reports the *same* AMUX_PORT reservation a script spawn gets — one
// allocator, one range per workspace, not a second booking.
func TestBuildSessionEnv_SharesScriptPortReservation(t *testing.T) {
	repo := t.TempDir()
	runner := NewScriptRunner(6200, 10)
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo

	scriptEnv, err := runner.buildScriptEnv(ws)
	if err != nil {
		t.Fatalf("buildScriptEnv() error = %v", err)
	}
	sessionEnv, err := runner.BuildSessionEnv(ws)
	if err != nil {
		t.Fatalf("BuildSessionEnv() error = %v", err)
	}
	if envSliceToMap(scriptEnv)["AMUX_PORT"] != envSliceToMap(sessionEnv)["AMUX_PORT"] {
		t.Fatalf("session AMUX_PORT = %q, script AMUX_PORT = %q — split reservations",
			envSliceToMap(sessionEnv)["AMUX_PORT"], envSliceToMap(scriptEnv)["AMUX_PORT"])
	}
}
