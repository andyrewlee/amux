package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXWorkspaceList_WorkspaceFilterShowsWorkspaceLabel(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"main","repo":"/tmp/demo","scope":"project","created":"2026-01-01T00:00:00Z"}],"error":null}'
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
		"--workspace", "ws-1",
	)

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	workspaceFilter, ok := data["workspace_filter"].(map[string]any)
	if !ok {
		t.Fatalf("workspace_filter missing or wrong type: %T", data["workspace_filter"])
	}
	if got, _ := workspaceFilter["id"].(string); got != "ws-1" {
		t.Fatalf("workspace_filter.id = %q, want %q", got, "ws-1")
	}
	if got, _ := workspaceFilter["label"].(string); got != "ws-1 (main) [project workspace]" {
		t.Fatalf("workspace_filter.label = %q, want %q", got, "ws-1 (main) [project workspace]")
	}

	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	if !strings.Contains(message, "Workspace filter: ws-1 (main) [project workspace]") {
		t.Fatalf("channel.message = %q, want workspace filter label", message)
	}
}

func TestOpenClawDXWorkspaceList_AllBypassesContextProjectFilter(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	argsLogPath := filepath.Join(fakeBinDir, "amux-args.log")
	contextPath := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(contextPath, []byte(`{"project":{"path":"/tmp/demo-context","name":"demo-context"}}`), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${ARGS_LOG_PATH:?missing ARGS_LOG_PATH}"
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"main","repo":"/tmp/demo-a","scope":"project","created":"2026-01-01T00:00:00Z"},{"id":"ws-2","name":"auth-fix","repo":"/tmp/demo-b","scope":"nested","parent_workspace":"ws-1","created":"2026-01-02T00:00:00Z"}],"error":null}'
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
	env = withEnv(env, "ARGS_LOG_PATH", argsLogPath)
	env = withEnv(env, "OPENCLAW_DX_CONTEXT_FILE", contextPath)

	payload := runScriptJSON(t, scriptPath, env,
		"workspace", "list",
		"--all",
	)

	argsRaw, err := os.ReadFile(argsLogPath)
	if err != nil {
		t.Fatalf("read amux args log: %v", err)
	}
	args := string(argsRaw)
	if !strings.Contains(args, "workspace list") {
		t.Fatalf("amux args = %q, want workspace list call", args)
	}
	if strings.Contains(args, "--repo /tmp/demo-context") {
		t.Fatalf("amux args = %q, did not expect context repo filter with --all", args)
	}

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	allProjects, _ := data["all_projects"].(bool)
	if !allProjects {
		t.Fatalf("data.all_projects = %v, want true", allProjects)
	}

	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	if !strings.Contains(message, "Project: all projects") {
		t.Fatalf("channel.message = %q, want all-projects hint", message)
	}
}

func TestOpenClawDXWorkspaceList_AllConflictsWithProject(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' '{"ok":true,"data":[],"error":null}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"workspace", "list",
		"--all",
		"--project", "/tmp/demo",
	)

	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "--all conflicts with --project") {
		t.Fatalf("summary = %q, want conflict explanation", summary)
	}
}
