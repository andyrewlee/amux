package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXStatus_PrioritizesNeedsInputAndWorkspaceLabels(t *testing.T) {
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
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-main","name":"mainline","repo":"/tmp/demo","scope":"project","created":"2026-01-01T00:00:00Z"},{"id":"ws-hotfix","name":"hotfix-login","repo":"/tmp/demo","scope":"nested","parent_workspace":"ws-main","created":"2026-01-02T00:00:00Z"}],"error":null}'
    ;;
  "agent list")
    printf '%s' '{"ok":true,"data":[{"session_name":"sess-hotfix","agent_id":"agent-hotfix","workspace_id":"ws-hotfix","tab_id":"tab-1","type":"agent"},{"session_name":"sess-main","agent_id":"agent-main","workspace_id":"ws-main","tab_id":"tab-2","type":"agent"}],"error":null}'
    ;;
  "terminal list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[{"session_name":"sess-hotfix"},{"session_name":"sess-main"}],"error":null}'
    ;;
  "session prune")
    printf '%s' '{"ok":true,"data":{"dry_run":true,"pruned":[],"total":0,"errors":[]},"error":null}'
    ;;
  "agent capture")
    if [[ "${3:-}" == "sess-main" ]]; then
      printf '%s' '{"ok":true,"data":{"session_name":"sess-main","status":"captured","summary":"Needs input: choose migration strategy.","needs_input":true,"input_hint":"Approve schema migration path"},"error":null}'
    else
      printf '%s' '{"ok":true,"data":{"session_name":"sess-hotfix","status":"captured","summary":"Implemented hotfix and tests passed.","needs_input":false,"input_hint":""},"error":null}'
    fi
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "status")

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	alerts, ok := data["alerts"].([]any)
	if !ok || len(alerts) < 2 {
		t.Fatalf("alerts missing or too short: %#v", data["alerts"])
	}
	firstAlert, ok := alerts[0].(map[string]any)
	if !ok {
		t.Fatalf("first alert wrong type: %T", alerts[0])
	}
	if got, _ := firstAlert["type"].(string); got != "needs_input" {
		t.Fatalf("first alert type = %q, want %q", got, "needs_input")
	}
	if got, _ := firstAlert["workspace_label"].(string); !strings.Contains(got, "ws-main (mainline) [project workspace]") {
		t.Fatalf("first alert workspace_label = %q, want project workspace label", got)
	}
	secondAlert, ok := alerts[1].(map[string]any)
	if !ok {
		t.Fatalf("second alert wrong type: %T", alerts[1])
	}
	if got, _ := secondAlert["type"].(string); got != "completed" {
		t.Fatalf("second alert type = %q, want %q", got, "completed")
	}
	if got, _ := secondAlert["workspace_label"].(string); !strings.Contains(got, "ws-hotfix (hotfix-login) [nested workspace <- ws-main]") {
		t.Fatalf("second alert workspace_label = %q, want nested workspace label", got)
	}

	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	needsIdx := strings.Index(message, "❓ ws-main (mainline) [project workspace]")
	completedIdx := strings.Index(message, "✅ ws-hotfix (hotfix-login) [nested workspace <- ws-main]")
	if needsIdx < 0 || completedIdx < 0 {
		t.Fatalf("channel.message = %q, want both labeled alert lines", message)
	}
	if needsIdx > completedIdx {
		t.Fatalf("channel.message = %q, want needs_input alert before completed alert", message)
	}
}

func TestOpenClawDXStatus_WorkspaceShowsAgentLinesAndContinueCommand(t *testing.T) {
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
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-main","name":"mainline","repo":"/tmp/demo","scope":"project","created":"2026-01-01T00:00:00Z"}],"error":null}'
    ;;
  "agent list")
    printf '%s' '{"ok":true,"data":[{"session_name":"sess-1","agent_id":"agent-1","workspace_id":"ws-main","tab_id":"tab-1","type":"agent"},{"session_name":"sess-2","agent_id":"agent-2","workspace_id":"ws-main","tab_id":"tab-2","type":"agent"}],"error":null}'
    ;;
  "terminal list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[{"session_name":"sess-1"},{"session_name":"sess-2"}],"error":null}'
    ;;
  "session prune")
    printf '%s' '{"ok":true,"data":{"dry_run":true,"pruned":[],"total":0,"errors":[]},"error":null}'
    ;;
  "agent capture")
    if [[ "${3:-}" == "sess-1" ]]; then
      printf '%s' '{"ok":true,"data":{"session_name":"sess-1","status":"captured","summary":"Investigating middleware refactor plan.","needs_input":false,"input_hint":""},"error":null}'
    else
      printf '%s' '{"ok":true,"data":{"session_name":"sess-2","status":"captured","summary":"Inspecting logging diagnostics flow.","needs_input":false,"input_hint":""},"error":null}'
    fi
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "status", "--workspace", "ws-main")

	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "openclaw-dx.sh continue --agent agent-1") {
		t.Fatalf("suggested_command = %q, want continue by first active agent", suggested)
	}

	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	if !strings.Contains(message, "Agents:") {
		t.Fatalf("channel.message = %q, want Agents section", message)
	}
	if !strings.Contains(message, "agent-1") || !strings.Contains(message, "agent-2") {
		t.Fatalf("channel.message = %q, want listed agent ids", message)
	}
}

func TestOpenClawDXStatus_AlertsOnlyAllClearSuggestsFullStatus(t *testing.T) {
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
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-main","name":"mainline","repo":"/tmp/demo","scope":"project","created":"2026-01-01T00:00:00Z"}],"error":null}'
    ;;
  "agent list")
    printf '%s' '{"ok":true,"data":[{"session_name":"sess-1","agent_id":"agent-1","workspace_id":"ws-main","tab_id":"tab-1","type":"agent"}],"error":null}'
    ;;
  "terminal list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[{"session_name":"sess-1"}],"error":null}'
    ;;
  "session prune")
    printf '%s' '{"ok":true,"data":{"dry_run":true,"pruned":[],"total":0,"errors":[]},"error":null}'
    ;;
  "agent capture")
    printf '%s' '{"ok":true,"data":{"session_name":"sess-1","status":"captured","summary":"Working through middleware cleanup.","needs_input":false,"input_hint":""},"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "status", "--alerts-only")

	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	suggested, _ := payload["suggested_command"].(string)
	if suggested != "skills/amux/scripts/openclaw-dx.sh status" {
		t.Fatalf("suggested_command = %q, want full status command", suggested)
	}
	nextAction, _ := payload["next_action"].(string)
	if !strings.Contains(nextAction, "Run full status view") {
		t.Fatalf("next_action = %q, want full status guidance", nextAction)
	}

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawFullStatus bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		cmd, _ := action["command"].(string)
		if id == "full_status" && cmd == "skills/amux/scripts/openclaw-dx.sh status" {
			sawFullStatus = true
			break
		}
	}
	if !sawFullStatus {
		t.Fatalf("expected full_status quick action in %#v", quickActions)
	}
}
