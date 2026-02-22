package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXWorkspaceCreate_RejectsProjectParentRepoMismatch(t *testing.T) {
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
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"parent-ws","name":"feature","repo":"/tmp/demo","assistant":"codex"}],"error":null}'
    ;;
  "workspace create")
    printf '%s' '{"ok":false,"error":{"code":"unexpected","message":"workspace create should not be called on mismatch"}}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"workspace", "create",
		"--name", "refactor",
		"--from-workspace", "parent-ws",
		"--project", "/tmp/other",
		"--scope", "nested",
	)

	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "--project does not match --from-workspace repository") {
		t.Fatalf("summary = %q, want mismatch guidance", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "workspace create --name refactor --from-workspace parent-ws --scope nested") {
		t.Fatalf("suggested_command = %q, want actionable mismatch retry command", suggested)
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawRetryNoProject bool
	var sawRetryParentRepo bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		switch id {
		case "retry_without_project":
			sawRetryNoProject = true
		case "retry_with_parent_repo":
			sawRetryParentRepo = true
		}
	}
	if !sawRetryNoProject || !sawRetryParentRepo {
		t.Fatalf("expected retry mismatch quick actions, got %#v", quickActions)
	}
}

func TestOpenClawDXWorkspaceCreate_ProjectScopeRejectsFromWorkspace(t *testing.T) {
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
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"parent-ws","name":"feature","repo":"/tmp/demo","assistant":"codex"}],"error":null}'
    ;;
  "workspace create")
    printf '%s' '{"ok":false,"error":{"code":"unexpected","message":"workspace create should not be called for project scope with parent"}}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"workspace", "create",
		"--name", "main",
		"--from-workspace", "parent-ws",
		"--scope", "project",
	)

	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "--from-workspace requires --scope nested") {
		t.Fatalf("summary = %q, want project-scope validation guidance", summary)
	}
}

func TestOpenClawDXWorkspaceCreate_ProjectNotRegisteredProvidesDXRecovery(t *testing.T) {
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
    printf '%s' '{"ok":false,"error":{"code":"project_not_registered","message":"project /tmp/api is not registered","details":{"project":"/tmp/api"}}}'
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
		"--name", "api-main",
		"--project", "/tmp/api",
		"--scope", "project",
		"--assistant", "codex",
	)

	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Project is not registered: /tmp/api") {
		t.Fatalf("summary = %q, want project registration guidance", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "project add --path /tmp/api") {
		t.Fatalf("suggested_command = %q, want openclaw-dx project add command", suggested)
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawAddProject bool
	var sawRetryCreate bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		switch id {
		case "add_project":
			sawAddProject = true
		case "retry_create":
			sawRetryCreate = true
		}
	}
	if !sawAddProject || !sawRetryCreate {
		t.Fatalf("expected add_project and retry_create quick actions, got %#v", quickActions)
	}
}
