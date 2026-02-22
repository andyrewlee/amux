package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXWorkspaceDecide_NestedPrefersContextWorkspaceAsParent(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	contextFile := filepath.Join(t.TempDir(), "context.json")

	if err := os.WriteFile(contextFile, []byte(`{"project":{"path":"/tmp/demo","name":"demo"},"workspace":{"id":"ws-context","name":"active-main","repo":"/tmp/demo","assistant":"codex","scope":"project","scope_label":"project workspace","parent_workspace":"","parent_name":""}}`), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
if [[ "${1:-}" == "workspace" && "${2:-}" == "list" && "${3:-}" == "--repo" ]]; then
  printf '%s' '{"ok":true,"data":[{"id":"ws-random","name":"legacy-main","repo":"/tmp/demo","assistant":"codex"},{"id":"ws-context","name":"active-main","repo":"/tmp/demo","assistant":"codex"},{"id":"ws-n1","name":"active-main.hotfix","repo":"/tmp/demo","assistant":"codex"}],"error":null}'
  exit 0
fi
if [[ "${1:-}" == "agent" && "${2:-}" == "list" ]]; then
  printf '%s' '{"ok":true,"data":[{"agent_id":"agent-1","workspace_id":"ws-context"}],"error":null}'
  exit 0
fi
if [[ "${1:-}" == "workspace" && "${2:-}" == "list" && "${3:-}" == "--archived" ]]; then
  printf '%s' '{"ok":true,"data":[{"id":"ws-random","name":"legacy-main","repo":"/tmp/demo","assistant":"codex"},{"id":"ws-context","name":"active-main","repo":"/tmp/demo","assistant":"codex"},{"id":"ws-n1","name":"active-main.hotfix","repo":"/tmp/demo","assistant":"codex"}],"error":null}'
  exit 0
fi
printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_CONTEXT_FILE", contextFile)

	payload := runScriptJSON(t, scriptPath, env,
		"workspace", "decide",
		"--project", "/tmp/demo",
		"--task", "Need a safe parallel patch.",
		"--assistant", "codex",
		"--name", "patch",
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
	if got, _ := data["parent_workspace"].(string); got != "ws-context" {
		t.Fatalf("parent_workspace = %q, want %q", got, "ws-context")
	}
	if got, _ := data["parent_selection_reason"].(string); got != "context_workspace" {
		t.Fatalf("parent_selection_reason = %q, want %q", got, "context_workspace")
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "--from-workspace ws-context") {
		t.Fatalf("suggested_command = %q, want context parent workspace", suggested)
	}
	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	if !strings.Contains(message, "Parent source: active workspace context") {
		t.Fatalf("channel.message = %q, want parent source context hint", message)
	}
}
