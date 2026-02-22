package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXProjectList_PaginatesAndAddsNavigationActions(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[{"name":"app-1","path":"/tmp/app-1"},{"name":"app-2","path":"/tmp/app-2"},{"name":"app-3","path":"/tmp/app-3"},{"name":"app-4","path":"/tmp/app-4"},{"name":"app-5","path":"/tmp/app-5"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"project", "list",
		"--limit", "2",
		"--page", "2",
	)

	if got, _ := payload["command"].(string); got != "project.list" {
		t.Fatalf("command = %q, want %q", got, "project.list")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["page"].(float64); got != 2 {
		t.Fatalf("page = %v, want 2", got)
	}
	if got, _ := data["total_pages"].(float64); got != 3 {
		t.Fatalf("total_pages = %v, want 3", got)
	}
	if got, _ := data["has_prev"].(bool); !got {
		t.Fatalf("has_prev = %v, want true", got)
	}
	if got, _ := data["has_next"].(bool); !got {
		t.Fatalf("has_next = %v, want true", got)
	}
	projectsPage, ok := data["projects_page"].([]any)
	if !ok || len(projectsPage) != 2 {
		t.Fatalf("projects_page = %#v, want len=2", data["projects_page"])
	}
	firstPageProject, ok := projectsPage[0].(map[string]any)
	if !ok {
		t.Fatalf("projects_page[0] wrong type: %T", projectsPage[0])
	}
	if got, _ := firstPageProject["name"].(string); got != "app-3" {
		t.Fatalf("projects_page[0].name = %q, want app-3", got)
	}

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok {
		t.Fatalf("quick_actions missing or wrong type: %T", payload["quick_actions"])
	}
	var sawPrev bool
	var sawNext bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		command, _ := action["command"].(string)
		switch id {
		case "prev_page":
			sawPrev = strings.Contains(command, "--page 1")
		case "next_page":
			sawNext = strings.Contains(command, "--page 3")
		}
	}
	if !sawPrev {
		t.Fatalf("expected prev_page quick action targeting page 1: %#v", quickActions)
	}
	if !sawNext {
		t.Fatalf("expected next_page quick action targeting page 3: %#v", quickActions)
	}
}

func TestOpenClawDXProjectList_QuickActionCallbackDataIsOpenClawSafe(t *testing.T) {
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
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"project", "list",
		"--limit", "1",
	)

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}

	seen := map[string]bool{}
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		callbackData, _ := action["callback_data"].(string)
		if !strings.HasPrefix(callbackData, "dx:") {
			t.Fatalf("callback_data = %q, want dx:* token", callbackData)
		}
		if len(callbackData) > 64 {
			t.Fatalf("callback_data len = %d, want <= 64 (%q)", len(callbackData), callbackData)
		}
		if seen[callbackData] {
			t.Fatalf("duplicate callback_data token: %q", callbackData)
		}
		seen[callbackData] = true
	}
}

func TestOpenClawDXProjectList_DataIncludesContextSnapshot(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	contextPath := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(contextPath, []byte(`{"project":{"path":"/tmp/demo","name":"demo"},"workspace":{"id":"ws-1","name":"mobile","repo":"/tmp/demo","assistant":"codex"},"agent":{"id":"agent-1","workspace_id":"ws-1","assistant":"codex"}}`), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project list")
    printf '%s' '{"ok":true,"data":[{"name":"demo","path":"/tmp/demo"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_CONTEXT_FILE", contextPath)

	payload := runScriptJSON(t, scriptPath, env, "project", "list")

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	context, ok := data["context"].(map[string]any)
	if !ok {
		t.Fatalf("data.context missing or wrong type: %T", data["context"])
	}
	project, ok := context["project"].(map[string]any)
	if !ok {
		t.Fatalf("context.project missing or wrong type: %T", context["project"])
	}
	if got, _ := project["path"].(string); got != "/tmp/demo" {
		t.Fatalf("context.project.path = %q, want /tmp/demo", got)
	}
}

func TestOpenClawDXWorkspaceList_UsesContextProjectWhenProjectMissing(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	argsLog := filepath.Join(fakeBinDir, "amux-args.log")
	contextPath := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(contextPath, []byte(`{"project":{"path":"/tmp/demo","name":"demo"}}`), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
printf '%s\n' "$*" >> "${ARGS_LOG:?missing ARGS_LOG}"
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"mobile","repo":"/tmp/demo","root":"/tmp/ws-1","assistant":"codex"}],"error":null}'
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
	env = withEnv(env, "ARGS_LOG", argsLog)
	env = withEnv(env, "OPENCLAW_DX_CONTEXT_FILE", contextPath)

	payload := runScriptJSON(t, scriptPath, env,
		"workspace", "list",
		"--limit", "1",
	)

	if got, _ := payload["command"].(string); got != "workspace.list" {
		t.Fatalf("command = %q, want %q", got, "workspace.list")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["project"].(string); got != "/tmp/demo" {
		t.Fatalf("project = %q, want /tmp/demo", got)
	}
	if got, _ := data["project_from_context"].(bool); !got {
		t.Fatalf("project_from_context = %v, want true", got)
	}

	argsRaw, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("read args log: %v", err)
	}
	if !strings.Contains(string(argsRaw), "workspace list --repo /tmp/demo") {
		t.Fatalf("workspace list did not use context project, args:\n%s", string(argsRaw))
	}
}
