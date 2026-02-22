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

func TestOpenClawDXWorkspaceCreate_FromWorkspaceAcceptsCanonicalProjectPathAlias(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[{"id":"parent-ws","name":"mobile-main","repo":"/Users/andrewlee/tmp/demo-web","assistant":"codex","scope":"project"}],"error":null}'
    ;;
  "project list")
    printf '%s' '{"ok":true,"data":[{"name":"demo-web","path":"/private/tmp/demo-web"}],"error":null}'
    ;;
  "workspace create")
    printf '%s' '{"ok":true,"data":{"id":"ws-nested","name":"mobile-main.auth-fix","repo":"/Users/andrewlee/tmp/demo-web","root":"/tmp/ws-nested","assistant":"codex"},"error":null}'
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
		"--name", "auth-fix",
		"--from-workspace", "parent-ws",
		"--scope", "nested",
		"--assistant", "codex",
	)

	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Nested workspace ready") {
		t.Fatalf("summary = %q, want nested workspace success summary", summary)
	}
}

func TestOpenClawDXWorkspaceCreate_ProjectNotRegisteredAutoRecoversFromWorkspaceRepo(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	projectAddLog := filepath.Join(fakeBinDir, "project-add.log")
	createCount := filepath.Join(fakeBinDir, "create-count.log")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"parent-ws","name":"mobile-main","repo":"/Users/andrewlee/tmp/demo-web","assistant":"codex","scope":"project"}],"error":null}'
    ;;
  "project list")
    printf '%s' '{"ok":true,"data":[{"name":"demo-web","path":"/private/tmp/demo-web"}],"error":null}'
    ;;
  "project add")
    printf '%s' "${3:-}" > "${PROJECT_ADD_LOG:?missing PROJECT_ADD_LOG}"
    printf '%s' '{"ok":true,"data":{"name":"demo-web","path":"/Users/andrewlee/tmp/demo-web"},"error":null}'
    ;;
  "workspace create")
    count=0
    if [[ -f "${CREATE_COUNT_LOG:?missing CREATE_COUNT_LOG}" ]]; then
      count="$(cat "${CREATE_COUNT_LOG}")"
    fi
    count=$((count + 1))
    printf '%s' "$count" > "${CREATE_COUNT_LOG}"
    if [[ "$count" -eq 1 ]]; then
      printf '%s' '{"ok":false,"error":{"code":"project_not_registered","message":"project /Users/andrewlee/tmp/demo-web is not registered; run amux project add /Users/andrewlee/tmp/demo-web first","details":{"project":"/Users/andrewlee/tmp/demo-web"}}}'
      exit 0
    fi
    printf '%s' '{"ok":true,"data":{"id":"ws-nested","name":"mobile-main.auth-fix","repo":"/Users/andrewlee/tmp/demo-web","root":"/tmp/ws-nested","assistant":"codex"},"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "PROJECT_ADD_LOG", projectAddLog)
	env = withEnv(env, "CREATE_COUNT_LOG", createCount)

	payload := runScriptJSON(t, scriptPath, env,
		"workspace", "create",
		"--name", "auth-fix",
		"--from-workspace", "parent-ws",
		"--scope", "nested",
		"--assistant", "codex",
	)

	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	projectAddRaw, err := os.ReadFile(projectAddLog)
	if err != nil {
		t.Fatalf("read project add log: %v", err)
	}
	if strings.TrimSpace(string(projectAddRaw)) != "/Users/andrewlee/tmp/demo-web" {
		t.Fatalf("project add path = %q, want /Users/andrewlee/tmp/demo-web", strings.TrimSpace(string(projectAddRaw)))
	}
	countRaw, err := os.ReadFile(createCount)
	if err != nil {
		t.Fatalf("read create count: %v", err)
	}
	if strings.TrimSpace(string(countRaw)) != "2" {
		t.Fatalf("workspace create call count = %q, want 2", strings.TrimSpace(string(countRaw)))
	}
}

func TestOpenClawDXWorkspaceList_ErrorsWhenWorkspaceFilterMissing(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"main","repo":"/tmp/demo","assistant":"codex"}],"error":null}'
    ;;
  "agent list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "terminal list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"workspace", "list",
		"--workspace", "ws-missing",
	)

	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "workspace not found") {
		t.Fatalf("summary = %q, want workspace-not-found guidance", summary)
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
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "project pick --") {
		t.Fatalf("suggested_command = %q, want project pick command", suggested)
	}
}

func TestOpenClawDXProjectPick_AutoSelectsCanonicalPathAliases(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[{"name":"demo-web","path":"/tmp/demo-web"},{"name":"demo-web","path":"/tmp/demo-web/"}],"error":null}'
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
		"--name", "demo-web",
	)

	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Selected project: demo-web") {
		t.Fatalf("summary = %q, want selected project summary", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "workspace create --name mobile --project /tmp/demo-web") {
		t.Fatalf("suggested_command = %q, want project path workspace-create command", suggested)
	}
}

func TestOpenClawDXProjectPick_PrefersActiveContextProjectWhenAmbiguous(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	contextPath := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(contextPath, []byte(`{"project":{"path":"/tmp/demo-web/"}}`), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project list")
    printf '%s' '{"ok":true,"data":[{"name":"demo-web","path":"/tmp/other-demo-web"},{"name":"demo-web","path":"/tmp/demo-web"},{"name":"demo-web","path":"/tmp/demo-web/"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_CONTEXT_FILE", contextPath)

	payload := runScriptJSON(t, scriptPath, env,
		"project", "pick",
		"--name", "demo-web",
	)

	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "--project /tmp/demo-web") {
		t.Fatalf("suggested_command = %q, want context project selection", suggested)
	}
}
