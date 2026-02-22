package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXWorkspaceCreate_NestedPersistsScopeLineage(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	contextPath := filepath.Join(t.TempDir(), "context.json")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-parent","name":"feature","repo":"/tmp/demo","root":"/tmp/ws-parent","assistant":"codex"}],"error":null}'
    ;;
  "workspace create")
    printf '%s' '{"ok":true,"data":{"id":"ws-child","name":"feature.fix","repo":"/tmp/demo","root":"/tmp/ws-child","assistant":"codex","base":"origin/main"},"error":null}'
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
		"workspace", "create",
		"--name", "fix",
		"--from-workspace", "ws-parent",
		"--scope", "nested",
		"--assistant", "codex",
	)

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["scope"].(string); got != "nested" {
		t.Fatalf("scope = %q, want %q", got, "nested")
	}
	if got, _ := data["scope_label"].(string); got != "nested workspace" {
		t.Fatalf("scope_label = %q, want %q", got, "nested workspace")
	}
	if got, _ := data["workspace_label"].(string); got != "ws-child (feature.fix) [nested workspace <- ws-parent]" {
		t.Fatalf("workspace_label = %q, want %q", got, "ws-child (feature.fix) [nested workspace <- ws-parent]")
	}
	if got, _ := data["parent_workspace_label"].(string); got != "ws-parent (feature) [project workspace]" {
		t.Fatalf("parent_workspace_label = %q, want %q", got, "ws-parent (feature) [project workspace]")
	}
	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	if !strings.Contains(message, "Parent workspace: ws-parent (feature) [project workspace]") {
		t.Fatalf("channel.message = %q, want parent workspace label", message)
	}

	contextRaw, err := os.ReadFile(contextPath)
	if err != nil {
		t.Fatalf("read context file: %v", err)
	}
	var contextPayload map[string]any
	if err := json.Unmarshal(contextRaw, &contextPayload); err != nil {
		t.Fatalf("decode context json: %v\nraw=%s", err, string(contextRaw))
	}
	lineage, ok := contextPayload["workspace_lineage"].(map[string]any)
	if !ok {
		t.Fatalf("workspace_lineage missing or wrong type: %T", contextPayload["workspace_lineage"])
	}
	entryRaw, ok := lineage["ws-child"]
	if !ok {
		t.Fatalf("workspace_lineage missing ws-child entry: %#v", lineage)
	}
	entry, ok := entryRaw.(map[string]any)
	if !ok {
		t.Fatalf("ws-child lineage entry wrong type: %T", entryRaw)
	}
	if got, _ := entry["scope"].(string); got != "nested" {
		t.Fatalf("lineage scope = %q, want %q", got, "nested")
	}
	if got, _ := entry["parent_workspace"].(string); got != "ws-parent" {
		t.Fatalf("lineage parent_workspace = %q, want %q", got, "ws-parent")
	}
}

func TestOpenClawDXWorkspaceList_AnnotatesWorkspaceTypes(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	contextPath := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(contextPath, []byte(`{"workspace_lineage":{"ws-parent":{"scope":"project"},"ws-child":{"scope":"nested","parent_workspace":"ws-parent","parent_name":"feature"}}}`), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-parent","name":"feature","repo":"/tmp/demo","root":"/tmp/ws-parent","assistant":"codex"},{"id":"ws-child","name":"feature.fix","repo":"/tmp/demo","root":"/tmp/ws-child","assistant":"codex"}],"error":null}'
    ;;
  "agent list")
    printf '%s' '{"ok":true,"data":[{"agent_id":"agent-1","workspace_id":"ws-child"}],"error":null}'
    ;;
  "terminal list")
    printf '%s' '{"ok":true,"data":[{"session_name":"term-1","workspace_id":"ws-parent"}],"error":null}'
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
		"workspace", "list",
		"--project", "/tmp/demo",
	)

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["project_scope_count"].(float64); got != 1 {
		t.Fatalf("project_scope_count = %v, want 1", got)
	}
	if got, _ := data["nested_scope_count"].(float64); got != 1 {
		t.Fatalf("nested_scope_count = %v, want 1", got)
	}
	workspaces, ok := data["workspaces"].([]any)
	if !ok {
		t.Fatalf("workspaces missing or wrong type: %T", data["workspaces"])
	}
	var sawNested bool
	for _, raw := range workspaces {
		ws, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := ws["id"].(string); id == "ws-child" {
			sawNested = true
			if got, _ := ws["scope"].(string); got != "nested" {
				t.Fatalf("ws-child scope = %q, want nested", got)
			}
			if got, _ := ws["scope_label"].(string); got != "nested workspace" {
				t.Fatalf("ws-child scope_label = %q, want nested workspace", got)
			}
			if got, _ := ws["parent_workspace"].(string); got != "ws-parent" {
				t.Fatalf("ws-child parent_workspace = %q, want ws-parent", got)
			}
		}
	}
	if !sawNested {
		t.Fatalf("did not find ws-child in workspaces: %#v", workspaces)
	}
}

func TestOpenClawDXWorkspaceList_NameInferenceWithoutParentDefaultsToProject(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[{"id":"ws-dot","name":"demo.v2","repo":"/tmp/demo","root":"/tmp/ws-dot","assistant":"codex"}],"error":null}'
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
		"--project", "/tmp/demo",
	)

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	workspaces, ok := data["workspaces"].([]any)
	if !ok || len(workspaces) == 0 {
		t.Fatalf("workspaces missing or empty: %#v", data["workspaces"])
	}
	ws, ok := workspaces[0].(map[string]any)
	if !ok {
		t.Fatalf("workspace row wrong type: %T", workspaces[0])
	}
	if got, _ := ws["scope"].(string); got != "project" {
		t.Fatalf("scope = %q, want %q", got, "project")
	}
	if got, _ := ws["scope_label"].(string); got != "project workspace" {
		t.Fatalf("scope_label = %q, want %q", got, "project workspace")
	}
	if got, _ := ws["scope_source"].(string); got != "name_inference" {
		t.Fatalf("scope_source = %q, want %q", got, "name_inference")
	}
	if got, _ := ws["parent_workspace"].(string); got != "" {
		t.Fatalf("parent_workspace = %q, want empty", got)
	}

	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	if !strings.Contains(message, "demo.v2  [project workspace]") {
		t.Fatalf("channel.message = %q, want project workspace label for dotted name without parent", message)
	}
}

func TestOpenClawDXStart_EmitsWorkspaceContextWithType(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-parent","name":"feature","repo":"/tmp/demo","root":"/tmp/ws-parent","assistant":"codex"},{"id":"ws-child","name":"feature.fix","repo":"/tmp/demo","root":"/tmp/ws-child","assistant":"codex"}],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":{},"error":null}'
    ;;
esac
`)

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"turn complete","agent_id":"agent-1","workspace_id":"ws-child","assistant":"codex","quick_actions":[],"channel":{"message":"done","chunks":["done"],"chunks_meta":[{"index":1,"total":1,"text":"done"}],"inline_buttons":[]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "OPENCLAW_PRESENT_SCRIPT", "/nonexistent")

	payload := runScriptJSON(t, scriptPath, env,
		"start",
		"--workspace", "ws-child",
		"--assistant", "codex",
		"--prompt", "Continue work",
	)

	workspaceContext, ok := payload["workspace_context"].(map[string]any)
	if !ok {
		t.Fatalf("workspace_context missing or wrong type: %T", payload["workspace_context"])
	}
	if got, _ := workspaceContext["id"].(string); got != "ws-child" {
		t.Fatalf("workspace_context.id = %q, want ws-child", got)
	}
	if got, _ := workspaceContext["scope"].(string); got != "nested" {
		t.Fatalf("workspace_context.scope = %q, want nested", got)
	}
	if got, _ := workspaceContext["parent_workspace"].(string); got != "ws-parent" {
		t.Fatalf("workspace_context.parent_workspace = %q, want ws-parent", got)
	}

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	dataContext, ok := data["workspace_context"].(map[string]any)
	if !ok {
		t.Fatalf("data.workspace_context missing or wrong type: %T", data["workspace_context"])
	}
	if got, _ := dataContext["scope_label"].(string); got != "nested workspace" {
		t.Fatalf("data.workspace_context.scope_label = %q, want nested workspace", got)
	}
	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	if message == "" || !strings.Contains(message, "Workspace: ws-child (feature.fix) [nested workspace <- ws-parent]") {
		t.Fatalf("channel.message = %q, want workspace context line", message)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "ws-child (feature.fix) [nested workspace <- ws-parent]") {
		t.Fatalf("summary = %q, want workspace label context", summary)
	}
}

func TestOpenClawDXStart_DoesNotPersistNameInferredWorkspaceLineage(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")
	contextPath := filepath.Join(t.TempDir(), "context.json")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-guess","name":"feature.fix","repo":"/tmp/demo","root":"/tmp/ws-guess","assistant":"codex"}],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":{},"error":null}'
    ;;
esac
`)

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"turn complete","agent_id":"agent-1","workspace_id":"ws-guess","assistant":"codex","quick_actions":[],"channel":{"message":"done","chunks":["done"],"chunks_meta":[{"index":1,"total":1,"text":"done"}],"inline_buttons":[]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "OPENCLAW_PRESENT_SCRIPT", "/nonexistent")
	env = withEnv(env, "OPENCLAW_DX_CONTEXT_FILE", contextPath)

	payload := runScriptJSON(t, scriptPath, env,
		"start",
		"--workspace", "ws-guess",
		"--assistant", "codex",
		"--prompt", "Continue work",
	)

	workspaceContext, ok := payload["workspace_context"].(map[string]any)
	if !ok {
		t.Fatalf("workspace_context missing or wrong type: %T", payload["workspace_context"])
	}
	if got, _ := workspaceContext["scope_source"].(string); got != "name_inference" {
		t.Fatalf("workspace_context.scope_source = %q, want name_inference", got)
	}

	contextRaw, err := os.ReadFile(contextPath)
	if err != nil {
		t.Fatalf("read context file: %v", err)
	}
	var contextPayload map[string]any
	if err := json.Unmarshal(contextRaw, &contextPayload); err != nil {
		t.Fatalf("decode context json: %v\nraw=%s", err, string(contextRaw))
	}
	lineageRaw, hasLineage := contextPayload["workspace_lineage"]
	if !hasLineage {
		return
	}
	lineage, ok := lineageRaw.(map[string]any)
	if !ok {
		t.Fatalf("workspace_lineage wrong type: %T", lineageRaw)
	}
	if _, exists := lineage["ws-guess"]; exists {
		t.Fatalf("workspace_lineage should not persist name-inferred ws-guess entry: %#v", lineage["ws-guess"])
	}
}

func TestOpenClawDXWorkspaceCreateConflict_DoesNotOverwriteExistingLineageScope(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	contextPath := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(contextPath, []byte(`{"workspace_lineage":{"ws-parent":{"scope":"project"},"ws-existing":{"scope":"nested","parent_workspace":"ws-parent","parent_name":"parent"}}}`), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

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
    printf '%s' '{"ok":true,"data":[{"id":"ws-parent","name":"parent","repo":"/tmp/demo","root":"/tmp/ws-parent","assistant":"codex"},{"id":"ws-existing","name":"parent.fix","repo":"/tmp/demo","root":"/tmp/ws-existing","assistant":"codex"}],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":{},"error":null}'
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_CONTEXT_FILE", contextPath)

	payload := runScriptJSON(t, scriptPath, env,
		"workspace", "create",
		"--name", "parent.fix",
		"--project", "/tmp/demo",
		"--scope", "project",
		"--assistant", "codex",
	)

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["existing_scope"].(string); got != "nested" {
		t.Fatalf("existing_scope = %q, want nested", got)
	}

	contextRaw, err := os.ReadFile(contextPath)
	if err != nil {
		t.Fatalf("read context file: %v", err)
	}
	var contextPayload map[string]any
	if err := json.Unmarshal(contextRaw, &contextPayload); err != nil {
		t.Fatalf("decode context json: %v\nraw=%s", err, string(contextRaw))
	}
	lineage, ok := contextPayload["workspace_lineage"].(map[string]any)
	if !ok {
		t.Fatalf("workspace_lineage missing or wrong type: %T", contextPayload["workspace_lineage"])
	}
	entryRaw, ok := lineage["ws-existing"]
	if !ok {
		t.Fatalf("workspace_lineage missing ws-existing entry: %#v", lineage)
	}
	entry, ok := entryRaw.(map[string]any)
	if !ok {
		t.Fatalf("ws-existing lineage entry wrong type: %T", entryRaw)
	}
	if got, _ := entry["scope"].(string); got != "nested" {
		t.Fatalf("lineage scope = %q, want nested", got)
	}
	if got, _ := entry["parent_workspace"].(string); got != "ws-parent" {
		t.Fatalf("lineage parent_workspace = %q, want ws-parent", got)
	}
}
