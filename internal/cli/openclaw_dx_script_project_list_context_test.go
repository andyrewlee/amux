package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXProjectList_SuggestsContextProjectPath(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	contextPath := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(contextPath, []byte(`{"project":{"path":"/tmp/demo/"}}`), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project list")
    printf '%s' '{"ok":true,"data":[{"name":"alpha","path":"/tmp/alpha"},{"name":"zeta","path":"/tmp/zeta"},{"name":"demo","path":"/tmp/demo"}],"error":null}'
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

	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "project pick --path /tmp/demo") {
		t.Fatalf("suggested_command = %q, want context project path selection", suggested)
	}

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawPickActive bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		command, _ := action["command"].(string)
		if id == "pick_active" && strings.Contains(command, "--path /tmp/demo") {
			sawPickActive = true
			break
		}
	}
	if !sawPickActive {
		t.Fatalf("expected pick_active quick action in %#v", quickActions)
	}
}

func TestOpenClawDXProjectList_ContextPayloadCompactsLineageByDefault(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	contextPath := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(contextPath, []byte(`{
  "project":{"path":"/tmp/demo","name":"demo"},
  "workspace":{"id":"ws-1","name":"mobile","repo":"/tmp/demo","assistant":"codex"},
  "agent":{"id":"agent-1","workspace_id":"ws-1","assistant":"codex"},
  "workspace_lineage":{
    "ws-1":{"scope":"nested","parent_workspace":"ws-parent","parent_name":"parent"},
    "ws-parent":{"scope":"project","parent_workspace":"","parent_name":""},
    "ws-other":{"scope":"nested","parent_workspace":"ws-parent","parent_name":"other"}
  }
}`), 0o644); err != nil {
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
	lineage, ok := context["workspace_lineage"].(map[string]any)
	if !ok {
		t.Fatalf("context.workspace_lineage missing or wrong type: %T", context["workspace_lineage"])
	}
	if _, exists := lineage["ws-1"]; !exists {
		t.Fatalf("context.workspace_lineage missing ws-1: %#v", lineage)
	}
	if _, exists := lineage["ws-parent"]; !exists {
		t.Fatalf("context.workspace_lineage missing ws-parent: %#v", lineage)
	}
	if _, exists := lineage["ws-other"]; exists {
		t.Fatalf("context.workspace_lineage should omit unrelated entries: %#v", lineage)
	}
}
