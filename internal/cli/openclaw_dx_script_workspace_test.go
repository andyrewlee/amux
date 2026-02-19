package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXWorkspaceCreate_NestedFromWorkspace(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	calledNameFile := filepath.Join(fakeBinDir, "called-name.txt")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"parent-ws","name":"feature","repo":"/tmp/demo","assistant":"codex"}],"error":null}'
    ;;
  "workspace create")
    ws_name="${3:-}"
    printf '%s' "$ws_name" > "${CALLED_NAME_FILE:?missing CALLED_NAME_FILE}"
    printf '{"ok":true,"data":{"id":"ws-nested","name":"%s","repo":"/tmp/demo","root":"/tmp/ws-nested","assistant":"codex"},"error":null}' "$ws_name"
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "CALLED_NAME_FILE", calledNameFile)

	payload := runScriptJSON(t, scriptPath, env,
		"workspace", "create",
		"--name", "refactor",
		"--from-workspace", "parent-ws",
		"--scope", "nested",
	)

	if got, _ := payload["command"].(string); got != "workspace.create" {
		t.Fatalf("command = %q, want %q", got, "workspace.create")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["final_name"].(string); got != "feature.refactor" {
		t.Fatalf("final_name = %q, want %q", got, "feature.refactor")
	}
	calledNameRaw, err := os.ReadFile(calledNameFile)
	if err != nil {
		t.Fatalf("read called name: %v", err)
	}
	if got := strings.TrimSpace(string(calledNameRaw)); got != "feature.refactor" {
		t.Fatalf("workspace create name = %q, want %q", got, "feature.refactor")
	}
}

func TestOpenClawDXProjectPick_DisambiguationUsesIndexSelectors(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[{"name":"repo","path":"/tmp/repo-a"},{"name":"repo","path":"/tmp/repo-b"},{"name":"other","path":"/tmp/other"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"project", "pick",
		"--name", "repo",
	)

	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawIndexSelect bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		cmd, _ := action["command"].(string)
		if strings.Contains(cmd, "project pick --index ") {
			sawIndexSelect = true
			break
		}
	}
	if !sawIndexSelect {
		t.Fatalf("expected index-based project pick command in quick actions: %#v", quickActions)
	}
}

func TestOpenClawDXProjectAdd_PropagatesStructuredAmuxError(t *testing.T) {
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
  "project add")
    printf '%s' '{"ok":false,"error":{"code":"add_failed","message":"project path already registered"}}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":{},"error":null}'
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"project", "add",
		"--path", "/tmp/demo",
	)

	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "project path already registered") {
		t.Fatalf("summary = %q, want propagated amux error message", summary)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	errData, ok := data["error"].(map[string]any)
	if !ok {
		t.Fatalf("data.error missing or wrong type: %T", data["error"])
	}
	if got, _ := errData["code"].(string); got != "add_failed" {
		t.Fatalf("error.code = %q, want %q", got, "add_failed")
	}
}

func TestOpenClawDXProjectAdd_InitialCommitGuidanceForWorkspaceCreate(t *testing.T) {
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
  "project add")
    printf '%s' '{"ok":true,"data":{"name":"demo","path":"/tmp/demo"},"error":null}'
    ;;
  "workspace create")
    printf '%s' '{"ok":false,"error":{"code":"create_failed","message":"git worktree add -b mobile /tmp/ws/mobile HEAD: fatal: invalid reference: HEAD"}}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":{},"error":null}'
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"project", "add",
		"--path", "/tmp/demo",
		"--workspace", "mobile",
	)

	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "no initial commit") {
		t.Fatalf("summary = %q, want initial-commit guidance", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "git -C /tmp/demo add -A") {
		t.Fatalf("suggested_command = %q, want git initial commit command", suggested)
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawRetry bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		if id == "retry" {
			sawRetry = true
			break
		}
	}
	if !sawRetry {
		t.Fatalf("expected retry quick action in %#v", quickActions)
	}
}

func TestOpenClawDXWorkspaceCreate_RecoversFromExistingBranchConflict(t *testing.T) {
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
  "workspace create")
    printf '%s' '{"ok":false,"error":{"code":"create_failed","message":"fatal: a branch named '\''main'\'' already exists"}}'
    ;;
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-main","name":"main","repo":"/tmp/demo","root":"/tmp/demo","assistant":"codex"}],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":false,"error":{"code":"unexpected","message":"unexpected args"}}'
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"workspace", "create",
		"--name", "main",
		"--project", "/tmp/demo",
		"--assistant", "codex",
	)

	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Workspace already exists") {
		t.Fatalf("summary = %q, want conflict recovery summary", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "start --workspace ws-main") {
		t.Fatalf("suggested_command = %q, want start on existing workspace", suggested)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	existing, ok := data["existing_workspace"].(map[string]any)
	if !ok {
		t.Fatalf("existing_workspace missing or wrong type: %T", data["existing_workspace"])
	}
	if got, _ := existing["id"].(string); got != "ws-main" {
		t.Fatalf("existing_workspace.id = %q, want %q", got, "ws-main")
	}
}
