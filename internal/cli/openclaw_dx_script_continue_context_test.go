package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXContinue_ContextWorkspaceWithoutAgentSurfacesOtherActiveAgents(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	contextPath := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(contextPath, []byte(`{"workspace":{"id":"ws-empty","name":"empty","scope":"project","scope_label":"project workspace","parent_workspace":"","parent_name":""}}`), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-} ${3:-}" in
  "agent list --workspace")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "agent list ")
    printf '%s' '{"ok":true,"data":[{"session_name":"sess-other","agent_id":"agent-other","workspace_id":"ws-other"},{"session_name":"sess-live","agent_id":"agent-live","workspace_id":"ws-live"}],"error":null}'
    ;;
  "workspace list --archived")
    printf '%s' '{"ok":true,"data":[{"id":"ws-empty","name":"empty","repo":"/tmp/demo","scope":"project","assistant":"claude"},{"id":"ws-live","name":"main.auth-fix","repo":"/tmp/demo/","scope":"nested","parent_workspace":"ws-main"},{"id":"ws-other","name":"other-task","repo":"/tmp/other","scope":"project"},{"id":"ws-unused","name":"unused","repo":"/tmp/demo","scope":"project"}],"error":null}'
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
		"continue",
		"--text", "Resume now.",
		"--enter",
	)

	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "--agent agent-live") {
		t.Fatalf("suggested_command = %q, want continue command for active agent", suggested)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["reason"].(string); got != "no_active_agent_selected_workspace_has_others" {
		t.Fatalf("reason = %q, want %q", got, "no_active_agent_selected_workspace_has_others")
	}
	if got, _ := data["same_repo_count"].(float64); got != 1 {
		t.Fatalf("same_repo_count = %v, want 1", got)
	}
	if got, _ := data["agents_shown"].(float64); got != 2 {
		t.Fatalf("agents_shown = %v, want 2", got)
	}
	if got, _ := data["agents_truncated"].(bool); got {
		t.Fatalf("agents_truncated = %v, want false", got)
	}
	workspaceDetails, ok := data["workspace_details"].(map[string]any)
	if !ok {
		t.Fatalf("workspace_details missing or wrong type: %T", data["workspace_details"])
	}
	if _, exists := workspaceDetails["ws-unused"]; exists {
		t.Fatalf("workspace_details should omit unrelated workspaces: %#v", workspaceDetails)
	}
	if _, exists := workspaceDetails["ws-empty"]; !exists {
		t.Fatalf("workspace_details missing selected workspace: %#v", workspaceDetails)
	}
	if _, exists := workspaceDetails["ws-live"]; !exists {
		t.Fatalf("workspace_details missing active workspace ws-live: %#v", workspaceDetails)
	}
	if _, exists := workspaceDetails["ws-other"]; !exists {
		t.Fatalf("workspace_details missing active workspace ws-other: %#v", workspaceDetails)
	}

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawContinue bool
	var sawAutoStart bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		cmd, _ := action["command"].(string)
		if strings.HasPrefix(id, "continue_") && strings.Contains(cmd, "--agent agent-live") {
			sawContinue = true
		}
		if id == "auto_start" && strings.Contains(cmd, "--workspace ws-empty --auto-start") && strings.Contains(cmd, "--assistant claude") {
			sawAutoStart = true
		}
	}
	if !sawContinue || !sawAutoStart {
		t.Fatalf("expected continue and auto_start actions in %#v", quickActions)
	}

	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	if !strings.Contains(message, "Active agents in other workspaces:") {
		t.Fatalf("channel.message = %q, want active-agents-in-other-workspaces guidance", message)
	}
	if !strings.Contains(message, "agent-live") {
		t.Fatalf("channel.message = %q, want active agent id", message)
	}
	if !strings.Contains(message, "same-project agents shown first") {
		t.Fatalf("channel.message = %q, want same-project hint", message)
	}
}

func TestOpenClawDXContinue_ExplicitWorkspaceDoesNotRedirectToOtherAgents(t *testing.T) {
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
case "${1:-} ${2:-} ${3:-}" in
  "agent list --workspace")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "agent list ")
    printf '%s' '{"ok":true,"data":[{"session_name":"sess-live","agent_id":"agent-live","workspace_id":"ws-live"}],"error":null}'
    ;;
  "workspace list --archived")
    printf '%s' '{"ok":true,"data":[{"id":"ws-explicit","name":"explicit","repo":"/tmp/demo","scope":"project","assistant":"claude"},{"id":"ws-live","name":"main.auth-fix","repo":"/tmp/demo","scope":"nested","parent_workspace":"ws-main"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"continue",
		"--workspace", "ws-explicit",
		"--text", "Resume now.",
		"--enter",
	)

	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["reason"].(string); got != "no_active_agent" {
		t.Fatalf("reason = %q, want %q", got, "no_active_agent")
	}

	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "continue --workspace ws-explicit --auto-start") {
		t.Fatalf("suggested_command = %q, want workspace auto-start suggestion", suggested)
	}
	if !strings.Contains(suggested, "--assistant claude") {
		t.Fatalf("suggested_command = %q, want workspace assistant", suggested)
	}
	if strings.Contains(suggested, "--agent agent-live") {
		t.Fatalf("suggested_command = %q, should not redirect to other agent when workspace is explicit", suggested)
	}
}

func TestOpenClawDXContinue_ContextWorkspaceQueryFailureSuggestsStatusAndAutoStart(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	contextPath := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(contextPath, []byte(`{"workspace":{"id":"ws-empty","name":"empty","scope":"project","scope_label":"project workspace","parent_workspace":"","parent_name":""}}`), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-} ${3:-}" in
  "agent list --workspace")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "agent list ")
    printf '%s' '{"ok":false,"error":{"code":"agent_list_failed","message":"backend unavailable"}}'
    ;;
  "workspace list --archived")
    printf '%s' '{"ok":true,"data":[{"id":"ws-empty","name":"empty","repo":"/tmp/demo","scope":"project"}],"error":null}'
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
		"continue",
		"--text", "Resume now.",
		"--enter",
	)

	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["reason"].(string); got != "no_active_agent_other_query_failed" {
		t.Fatalf("reason = %q, want %q", got, "no_active_agent_other_query_failed")
	}

	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "openclaw-dx.sh status") {
		t.Fatalf("suggested_command = %q, want global status suggestion", suggested)
	}

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawStatus bool
	var sawAutoStart bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		cmd, _ := action["command"].(string)
		if id == "status" && strings.Contains(cmd, "openclaw-dx.sh status") {
			sawStatus = true
		}
		if id == "auto_start" && strings.Contains(cmd, "--workspace ws-empty --auto-start") {
			sawAutoStart = true
		}
	}
	if !sawStatus || !sawAutoStart {
		t.Fatalf("expected status and auto_start actions in %#v", quickActions)
	}

	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	if !strings.Contains(message, "Could not query active agents in other workspaces right now.") {
		t.Fatalf("channel.message = %q, want query-failure hint", message)
	}
}

func TestOpenClawDXContinue_ContextWorkspaceOtherAgentsHeaderShowsTruncation(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	contextPath := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(contextPath, []byte(`{"workspace":{"id":"ws-empty","name":"empty","scope":"project","scope_label":"project workspace","parent_workspace":"","parent_name":""}}`), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-} ${3:-}" in
  "agent list --workspace")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "agent list ")
    printf '%s' '{"ok":true,"data":[{"agent_id":"agent-1","workspace_id":"ws-1"},{"agent_id":"agent-2","workspace_id":"ws-2"},{"agent_id":"agent-3","workspace_id":"ws-3"},{"agent_id":"agent-4","workspace_id":"ws-4"},{"agent_id":"agent-5","workspace_id":"ws-5"},{"agent_id":"agent-6","workspace_id":"ws-6"},{"agent_id":"agent-7","workspace_id":"ws-7"}],"error":null}'
    ;;
  "workspace list --archived")
    printf '%s' '{"ok":true,"data":[{"id":"ws-empty","name":"empty","repo":"/tmp/demo","scope":"project"},{"id":"ws-1","name":"w1","repo":"/tmp/other","scope":"project"},{"id":"ws-2","name":"w2","repo":"/tmp/other","scope":"project"},{"id":"ws-3","name":"w3","repo":"/tmp/other","scope":"project"},{"id":"ws-4","name":"w4","repo":"/tmp/other","scope":"project"},{"id":"ws-5","name":"w5","repo":"/tmp/other","scope":"project"},{"id":"ws-6","name":"w6","repo":"/tmp/other","scope":"project"},{"id":"ws-7","name":"w7","repo":"/tmp/other","scope":"project"}],"error":null}'
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
		"continue",
		"--text", "Resume now.",
		"--enter",
	)

	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	if !strings.Contains(message, "Active agents in other workspaces (showing 6 of 7):") {
		t.Fatalf("channel.message = %q, want truncation header", message)
	}
}

func TestOpenClawDXContinue_NoTargetPrioritizesContextProjectAgents(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	contextPath := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(contextPath, []byte(`{"project":{"path":"/tmp/demo-target","name":"demo-target"}}`), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-} ${3:-}" in
  "agent list ")
    printf '%s' '{"ok":true,"data":[{"agent_id":"agent-other","workspace_id":"ws-other"},{"agent_id":"agent-target","workspace_id":"ws-target"}],"error":null}'
    ;;
  "workspace list --archived")
    printf '%s' '{"ok":true,"data":[{"id":"ws-other","name":"other","repo":"/tmp/other","scope":"project"},{"id":"ws-target","name":"target","repo":"/tmp/demo-target/","scope":"project"}],"error":null}'
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
		"continue",
		"--text", "Resume now.",
		"--enter",
	)

	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "--agent agent-target") {
		t.Fatalf("suggested_command = %q, want prioritized context-project agent", suggested)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "1 in active project") {
		t.Fatalf("summary = %q, want active-project count hint", summary)
	}

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["same_project_count"].(float64); got != 1 {
		t.Fatalf("same_project_count = %v, want 1", got)
	}

	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	if !strings.Contains(message, "context-project agents shown first") {
		t.Fatalf("channel.message = %q, want context-project ordering hint", message)
	}
}

func TestOpenClawDXContinue_NoTargetMultipleAgentsShowsTruncationHeader(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	contextPath := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(contextPath, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-} ${3:-}" in
  "agent list ")
    printf '%s' '{"ok":true,"data":[{"agent_id":"agent-1","workspace_id":"ws-1"},{"agent_id":"agent-2","workspace_id":"ws-2"},{"agent_id":"agent-3","workspace_id":"ws-3"},{"agent_id":"agent-4","workspace_id":"ws-4"},{"agent_id":"agent-5","workspace_id":"ws-5"},{"agent_id":"agent-6","workspace_id":"ws-6"},{"agent_id":"agent-7","workspace_id":"ws-7"}],"error":null}'
    ;;
  "workspace list --archived")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"w1","repo":"/tmp/a","scope":"project"},{"id":"ws-2","name":"w2","repo":"/tmp/b","scope":"project"},{"id":"ws-3","name":"w3","repo":"/tmp/c","scope":"project"},{"id":"ws-4","name":"w4","repo":"/tmp/d","scope":"project"},{"id":"ws-5","name":"w5","repo":"/tmp/e","scope":"project"},{"id":"ws-6","name":"w6","repo":"/tmp/f","scope":"project"},{"id":"ws-7","name":"w7","repo":"/tmp/g","scope":"project"}],"error":null}'
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
		"continue",
		"--text", "Resume now.",
		"--enter",
	)

	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	if !strings.Contains(message, "Multiple active agents found (showing 6 of 7)") {
		t.Fatalf("channel.message = %q, want truncation header", message)
	}
}
