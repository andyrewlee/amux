package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	if got, _ := data["existing_workspace_label"].(string); got != "ws-main (main) [project workspace]" {
		t.Fatalf("existing_workspace_label = %q, want %q", got, "ws-main (main) [project workspace]")
	}
	existing, ok := data["existing_workspace"].(map[string]any)
	if !ok {
		t.Fatalf("existing_workspace missing or wrong type: %T", data["existing_workspace"])
	}
	if got, _ := existing["id"].(string); got != "ws-main" {
		t.Fatalf("existing_workspace.id = %q, want %q", got, "ws-main")
	}
	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	if !strings.Contains(message, "Reusing existing project workspace: ws-main (main) [project workspace]") {
		t.Fatalf("channel.message = %q, want existing workspace label", message)
	}
}

func TestOpenClawDXWorkspaceCreate_ConflictRecoveryPrefersExactNameMatch(t *testing.T) {
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
    printf '%s' '{"ok":false,"error":{"code":"create_failed","message":"workspace already exists"}}'
    ;;
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-main","name":"main","repo":"/tmp/demo","root":"/tmp/ws-main","assistant":"codex"},{"id":"ws-main-auth","name":"main.auth-fix","repo":"/tmp/demo","root":"/tmp/ws-main-auth","assistant":"codex"}],"error":null}'
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
		"--name", "main.auth-fix",
		"--project", "/tmp/demo",
		"--scope", "project",
		"--assistant", "codex",
	)

	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	existing, ok := data["existing_workspace"].(map[string]any)
	if !ok {
		t.Fatalf("existing_workspace missing or wrong type: %T", data["existing_workspace"])
	}
	if got, _ := existing["id"].(string); got != "ws-main-auth" {
		t.Fatalf("existing_workspace.id = %q, want %q", got, "ws-main-auth")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "ws-main-auth") {
		t.Fatalf("summary = %q, want exact-name workspace id", summary)
	}
}

func TestOpenClawDXWorkspaceCreate_ConflictWithoutMatchSuggestsNewName(t *testing.T) {
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
    printf '%s' '{"ok":false,"error":{"code":"create_failed","message":"worktree add with new branch failed: fatal: '\''/tmp/amux/demo/main.auth-fix'\'' already exists"}}'
    ;;
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"parent-ws","name":"main","repo":"/tmp/demo","root":"/tmp/ws-main","assistant":"codex","scope":"project"},{"id":"ws-other","name":"main.refactor","repo":"/tmp/demo","root":"/tmp/ws-main-refactor","assistant":"codex","scope":"nested","parent_workspace":"parent-ws","parent_name":"main"}],"error":null}'
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
		"--name", "auth-fix",
		"--from-workspace", "parent-ws",
		"--scope", "nested",
		"--assistant", "codex",
	)

	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Workspace name is unavailable: main.auth-fix") {
		t.Fatalf("summary = %q, want name-unavailable summary", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "--name auth-fix-2 --from-workspace parent-ws --scope nested") {
		t.Fatalf("suggested_command = %q, want nested retry command with new name", suggested)
	}
}
