package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXGitShip_NoChangesWithPushPushesAheadCommits(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")

	repoDir := t.TempDir()
	if out, err := exec.Command("git", "-C", repoDir, "init", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "config", "user.email", "dx@example.com").CombinedOutput(); err != nil {
		t.Fatalf("git config email: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "config", "user.name", "DX Bot").CombinedOutput(); err != nil {
		t.Fatalf("git config name: %v\n%s", err, string(out))
	}

	remoteDir := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", remoteDir).CombinedOutput(); err != nil {
		t.Fatalf("git init bare: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "remote", "add", "origin", remoteDir).CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, string(out))
	}

	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	if out, err := exec.Command("git", "-C", repoDir, "add", "README.md").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "commit", "-m", "initial").CombinedOutput(); err != nil {
		t.Fatalf("git commit initial: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "push", "-u", "origin", "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("git push initial: %v\n%s", err, string(out))
	}

	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("modify README: %v", err)
	}
	if out, err := exec.Command("git", "-C", repoDir, "add", "README.md").CombinedOutput(); err != nil {
		t.Fatalf("git add second: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "commit", "-m", "second").CombinedOutput(); err != nil {
		t.Fatalf("git commit second: %v\n%s", err, string(out))
	}

	localHeadBeforePush, err := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse local: %v", err)
	}
	remoteHeadBeforePush, err := exec.Command("git", "--git-dir", remoteDir, "rev-parse", "refs/heads/main").Output()
	if err != nil {
		t.Fatalf("git rev-parse remote before push: %v", err)
	}
	if strings.TrimSpace(string(localHeadBeforePush)) == strings.TrimSpace(string(remoteHeadBeforePush)) {
		t.Fatalf("expected local HEAD to be ahead before push")
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' "${FAKE_WORKSPACE_LIST_JSON:?missing FAKE_WORKSPACE_LIST_JSON}"
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	workspaceListJSON := `{"ok":true,"data":[{"id":"ws-1","name":"demo","repo":"` + repoDir + `","root":"` + repoDir + `"}],"error":null}`
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "FAKE_WORKSPACE_LIST_JSON", workspaceListJSON)

	payload := runScriptJSON(t, scriptPath, env,
		"git", "ship",
		"--workspace", "ws-1",
		"--push",
	)

	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "pushed existing commits") {
		t.Fatalf("summary = %q, want pushed existing commits", summary)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if pushed, _ := data["pushed"].(bool); !pushed {
		t.Fatalf("pushed = %v, want true", pushed)
	}

	remoteHeadAfterPush, err := exec.Command("git", "--git-dir", remoteDir, "rev-parse", "refs/heads/main").Output()
	if err != nil {
		t.Fatalf("git rev-parse remote after push: %v", err)
	}
	if strings.TrimSpace(string(localHeadBeforePush)) != strings.TrimSpace(string(remoteHeadAfterPush)) {
		t.Fatalf("expected remote HEAD to match local HEAD after push")
	}
}

func TestOpenClawDXWorkspaceDecide_RecommendsNestedFromParent(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
if [[ "${1:-}" == "workspace" && "${2:-}" == "list" && "${3:-}" == "--archived" ]]; then
  printf '%s' '{"ok":true,"data":[{"id":"ws-parent","name":"feature","repo":"/tmp/demo","assistant":"codex"}],"error":null}'
  exit 0
fi
if [[ "${1:-}" == "workspace" && "${2:-}" == "list" && "${3:-}" == "--repo" ]]; then
  printf '%s' '{"ok":true,"data":[{"id":"ws-parent","name":"feature","repo":"/tmp/demo","assistant":"codex"}],"error":null}'
  exit 0
fi
if [[ "${1:-}" == "agent" && "${2:-}" == "list" ]]; then
  printf '%s' '{"ok":true,"data":[{"agent_id":"agent-1","workspace_id":"ws-parent"}],"error":null}'
  exit 0
fi
printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"workspace", "decide",
		"--from-workspace", "ws-parent",
		"--task", "Refactor largest tech debt area.",
		"--assistant", "codex",
		"--name", "refactor",
	)

	if got, _ := payload["command"].(string); got != "workspace.decide" {
		t.Fatalf("command = %q, want %q", got, "workspace.decide")
	}
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["recommendation"].(string); got != "nested" {
		t.Fatalf("recommendation = %q, want %q", got, "nested")
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "workflow kickoff") || !strings.Contains(suggested, "--scope nested") {
		t.Fatalf("suggested_command = %q, want nested kickoff", suggested)
	}
}

func TestOpenClawDXWorkspaceDecide_ProjectOnlyDoesNotRequireAlternateCommand(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
if [[ "${1:-}" == "workspace" && "${2:-}" == "list" && "${3:-}" == "--repo" ]]; then
  printf '%s' '{"ok":true,"data":[],"error":null}'
  exit 0
fi
if [[ "${1:-}" == "agent" && "${2:-}" == "list" ]]; then
  printf '%s' '{"ok":true,"data":[],"error":null}'
  exit 0
fi
printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"workspace", "decide",
		"--project", "/tmp/demo",
		"--task", "Ship initial feature set.",
		"--assistant", "codex",
		"--name", "mainline",
	)

	if got, _ := payload["command"].(string); got != "workspace.decide" {
		t.Fatalf("command = %q, want %q", got, "workspace.decide")
	}
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["recommendation"].(string); got != "project" {
		t.Fatalf("recommendation = %q, want %q", got, "project")
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "--project /tmp/demo") {
		t.Fatalf("suggested_command = %q, want project kickoff command", suggested)
	}
}

func TestOpenClawDXTerminalPreset_StartsNextJSPreset(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	argsLog := filepath.Join(fakeBinDir, "terminal-args.log")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
printf '%s\n' "$*" > "${TERMINAL_ARGS_LOG:?missing TERMINAL_ARGS_LOG}"
if [[ "${1:-}" == "terminal" && "${2:-}" == "run" ]]; then
  printf '%s' '{"ok":true,"data":{"session_name":"term-1","created":true},"error":null}'
  exit 0
fi
printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "TERMINAL_ARGS_LOG", argsLog)

	payload := runScriptJSON(t, scriptPath, env,
		"terminal", "preset",
		"--workspace", "ws-1",
		"--kind", "nextjs",
		"--manager", "pnpm",
		"--port", "3100",
		"--host", "127.0.0.1",
	)

	if got, _ := payload["command"].(string); got != "terminal.preset" {
		t.Fatalf("command = %q, want %q", got, "terminal.preset")
	}
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["session_name"].(string); got != "term-1" {
		t.Fatalf("session_name = %q, want %q", got, "term-1")
	}
	argsRaw, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("read terminal args: %v", err)
	}
	args := strings.TrimSpace(string(argsRaw))
	if !strings.Contains(args, "terminal run --workspace ws-1") ||
		!strings.Contains(args, `NEXT_TELEMETRY_DISABLED=1; pnpm dev -- --port "3100" --hostname "127.0.0.1"`) ||
		!strings.Contains(args, "--enter=true") {
		t.Fatalf("terminal args = %q, expected workspace/pnpm/port/host/enter", args)
	}
}

func TestOpenClawDXStatus_SurfacesCompletedAlertAndReviewActions(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project list")
    printf '%s' '{"ok":true,"data":[{"name":"demo","path":"/tmp/demo"}],"error":null}'
    ;;
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"demo","repo":"/tmp/demo","created":"2026-01-01T00:00:00Z"}],"error":null}'
    ;;
  "agent list")
    printf '%s' '{"ok":true,"data":[{"session_name":"sess-1","agent_id":"agent-1","workspace_id":"ws-1","tab_id":"tab-1","type":"agent"}],"error":null}'
    ;;
  "terminal list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[{"session_name":"sess-1","workspace_id":"ws-1","type":"agent","attached":false,"age_seconds":100}],"error":null}'
    ;;
  "session prune")
    printf '%s' '{"ok":true,"data":{"dry_run":true,"pruned":[],"total":0,"errors":[]},"error":null}'
    ;;
  "agent capture")
    printf '%s' '{"ok":true,"data":{"session_name":"sess-1","status":"captured","summary":"Implemented fix and tests passed.","needs_input":false,"input_hint":""},"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "status", "--workspace", "ws-1")

	if got, _ := payload["command"].(string); got != "status" {
		t.Fatalf("command = %q, want %q", got, "status")
	}
	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "review --workspace ws-1") {
		t.Fatalf("suggested_command = %q, want review command", suggested)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	alerts, ok := data["alerts"].([]any)
	if !ok || len(alerts) == 0 {
		t.Fatalf("alerts missing or empty: %#v", data["alerts"])
	}
	var sawCompleted bool
	for _, raw := range alerts {
		alert, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if alert["type"] == "completed" {
			sawCompleted = true
			break
		}
	}
	if !sawCompleted {
		t.Fatalf("expected completed alert in %#v", alerts)
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok {
		t.Fatalf("quick_actions missing or wrong type: %T", payload["quick_actions"])
	}
	var sawReviewDone bool
	var sawShipDone bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		if id == "review_done" {
			sawReviewDone = true
		}
		if id == "ship_done" {
			sawShipDone = true
		}
	}
	if !sawReviewDone || !sawShipDone {
		t.Fatalf("expected review_done and ship_done quick actions, got %#v", quickActions)
	}
}
