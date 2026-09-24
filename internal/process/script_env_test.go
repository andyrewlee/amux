package process

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

func envSliceToMap(env []string) map[string]string {
	out := map[string]string{}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			out[k] = v
		}
	}
	return out
}

func TestBuildScriptEnv_LayerPrecedence(t *testing.T) {
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"env": {"SHARED": "repo", "REPO_ONLY": "r"}}`)
	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo)
	runner.SetProjectEnvResolver(func(string) map[string]string {
		return map[string]string{"SHARED": "project", "PROJ_ONLY": "p"}
	})
	ws := &data.Workspace{
		Name: "ws", Repo: repo, Root: t.TempDir(),
		ScriptMode: "nonconcurrent",
		Env:        map[string]string{"SHARED": "ws"},
	}

	env, err := runner.buildScriptEnv(ws)
	if err != nil {
		t.Fatalf("buildScriptEnv() error = %v", err)
	}
	m := envSliceToMap(env)
	if m["SHARED"] != "ws" {
		t.Fatalf("SHARED = %q, want ws (repo < project < ws)", m["SHARED"])
	}
	if m["REPO_ONLY"] != "r" || m["PROJ_ONLY"] != "p" {
		t.Fatalf("repo/project-only vars missing: %v", m)
	}
}

func TestBuildScriptEnv_UntrustedRepoEnvGated(t *testing.T) {
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"env": {"X": "1"}}`)
	runner := NewScriptRunner(6200, 10)
	useTempTrust(t, runner) // isolated, trust withheld
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo

	if _, err := runner.buildScriptEnv(ws); !errors.Is(err, ErrScriptsNotTrusted) {
		t.Fatalf("buildScriptEnv() error = %v, want ErrScriptsNotTrusted", err)
	}
}

func TestRunScript_UntrustedRepoEnvBlocksUserScript(t *testing.T) {
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"env": {"X": "1"}}`)
	runner := NewScriptRunner(6200, 10)
	useTempTrust(t, runner)
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo
	// The command itself is user-entered (trust-exempt by design) — but repo
	// env reaching the spawned process must still be gated.
	ws.Scripts.Run = "touch user-ran"

	if _, err := runner.RunScript(ws, ScriptRun); !errors.Is(err, ErrScriptsNotTrusted) {
		t.Fatalf("RunScript() error = %v, want ErrScriptsNotTrusted", err)
	}
	if _, statErr := os.Stat(filepath.Join(ws.Root, "user-ran")); statErr == nil {
		t.Fatal("user script ran while repo env was untrusted")
	}
}

func TestRunScript_TrustedRepoAndProjectEnvFlow(t *testing.T) {
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"env": {"REPO_VAR": "rv", "SHARED": "repo"}}`)
	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo)
	runner.SetProjectEnvResolver(func(string) map[string]string {
		return map[string]string{"PROJ_VAR": "pv", "SHARED": "project"}
	})
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo
	ws.Scripts.Run = `printf '%s|%s|%s' "$REPO_VAR" "$PROJ_VAR" "$SHARED" > env-out`

	if _, err := runner.RunScript(ws, ScriptRun); err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}
	out := filepath.Join(ws.Root, "env-out")
	if err := waitForFile(out, 3*time.Second); err != nil {
		t.Fatalf("script output never appeared: %v", err)
	}
	got, _ := os.ReadFile(out)
	if string(got) != "rv|pv|project" {
		t.Fatalf("env values = %q, want rv|pv|project", got)
	}
}

func TestBuildScriptEnv_ReservedKeysFilteredFromLayers(t *testing.T) {
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"env": {"AMUX_WORKSPACE_ROOT": "/evil", "ORDINARY": "ok"}}`)
	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo)
	runner.SetProjectEnvResolver(func(string) map[string]string {
		return map[string]string{"AMUX_PORT": "1"}
	})
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo
	ws.Env = map[string]string{"ROOT_WORKSPACE_PATH": "/also-evil"}

	env, err := runner.buildScriptEnv(ws)
	if err != nil {
		t.Fatalf("buildScriptEnv() error = %v", err)
	}
	m := envSliceToMap(env)
	if m["AMUX_WORKSPACE_ROOT"] == "/evil" || m["ROOT_WORKSPACE_PATH"] == "/also-evil" || m["AMUX_PORT"] == "1" {
		t.Fatalf("reserved key overrode an injected value: %v", m)
	}
	if m["AMUX_WORKSPACE_ROOT"] != ws.Root {
		t.Fatalf("AMUX_WORKSPACE_ROOT = %q, want injected %q", m["AMUX_WORKSPACE_ROOT"], ws.Root)
	}
	if m["ORDINARY"] != "ok" {
		t.Fatal("ordinary repo env key lost")
	}
}

func TestBuildScriptEnv_NoLayersUnchanged(t *testing.T) {
	repo := t.TempDir() // no config file at all
	runner := NewScriptRunner(6200, 10)
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo
	ws.Env = map[string]string{"ONLY": "ws"}

	env, err := runner.buildScriptEnv(ws)
	if err != nil {
		t.Fatalf("buildScriptEnv() error = %v", err)
	}
	if envSliceToMap(env)["ONLY"] != "ws" {
		t.Fatal("ws.Env missing from env")
	}
}

// TestBuildScriptEnv_PortExhaustionReturnsError proves allocator exhaustion
// reaches the caller as ErrPortRangeExhausted — the typed spawn failure the
// Cmd layer wraps into its lifecycle result — instead of a panic.
func TestBuildScriptEnv_PortExhaustionReturnsError(t *testing.T) {
	repo := t.TempDir()
	runner := NewScriptRunner(1, 32768) // range covers the entire port space
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo
	ws.Root = t.TempDir()

	other := *ws
	other.Root = t.TempDir()
	if _, err := runner.buildScriptEnv(&other); err != nil {
		t.Fatalf("first buildScriptEnv() error = %v", err)
	}
	if _, err := runner.buildScriptEnv(ws); !errors.Is(err, ErrPortRangeExhausted) {
		t.Fatalf("exhausted buildScriptEnv() error = %v, want ErrPortRangeExhausted", err)
	}
}
