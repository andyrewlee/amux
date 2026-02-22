package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXContinue_NoTargetWithMultipleAgentsPromptsSelection(t *testing.T) {
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
  "agent list")
    printf '%s' '{"ok":true,"data":[{"session_name":"sess-a","agent_id":"agent-a","workspace_id":"ws-a"},{"session_name":"sess-b","agent_id":"agent-b","workspace_id":"ws-b"}],"error":null}'
    ;;
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-a","name":"main","repo":"/tmp/demo","assistant":"codex"},{"id":"ws-b","name":"main.auth-fix","repo":"/tmp/demo","assistant":"codex"}],"error":null}'
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
	if !strings.Contains(suggested, "--agent agent-a") {
		t.Fatalf("suggested_command = %q, want agent selection command", suggested)
	}

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["reason"].(string); got != "multiple_active_agents" {
		t.Fatalf("reason = %q, want %q", got, "multiple_active_agents")
	}

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawFirst bool
	var sawSecond bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		cmd, _ := action["command"].(string)
		if id == "continue_1" && strings.Contains(cmd, "--agent agent-a") {
			sawFirst = true
		}
		if id == "continue_2" && strings.Contains(cmd, "--agent agent-b") {
			sawSecond = true
		}
	}
	if !sawFirst || !sawSecond {
		t.Fatalf("expected continue actions for both agents: %#v", quickActions)
	}

	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	if !strings.Contains(message, "ws-a (main) [project workspace]") {
		t.Fatalf("channel.message = %q, want project workspace context", message)
	}
	if !strings.Contains(message, "ws-b (main.auth-fix) [nested workspace <- ws-a]") {
		t.Fatalf("channel.message = %q, want nested workspace context", message)
	}
}
