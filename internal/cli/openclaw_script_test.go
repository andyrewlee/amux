package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runScriptJSONWithInput(t *testing.T, scriptPath string, env []string, input string, args ...string) map[string]any {
	t.Helper()
	cmd := exec.Command(scriptPath, args...)
	cmd.Env = env
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v failed: %v", scriptPath, args, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}
	return payload
}

func TestOpenClawPresentScript_AugmentsChannelEnvelope(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-present.sh")
	env := withEnv(os.Environ(), "OPENCLAW_CHANNEL", "msteams")
	input := `{"ok":true,"summary":"ok","message":"Build complete","quick_actions":[{"id":"status","label":"Status","command":"amux --json status","style":"primary","prompt":"Check status"}],"channel":{"message":"Build complete","chunks_meta":[{"index":1,"total":1,"text":"Build complete"}]}}`

	payload := runScriptJSONWithInput(t, scriptPath, env, input)

	openclaw, ok := payload["openclaw"].(map[string]any)
	if !ok {
		t.Fatalf("openclaw missing or wrong type: %T", payload["openclaw"])
	}
	if got, _ := openclaw["selected_channel"].(string); got != "msteams" {
		t.Fatalf("openclaw.selected_channel = %q, want msteams", got)
	}
	presentation, ok := openclaw["presentation"].(map[string]any)
	if !ok {
		t.Fatalf("openclaw.presentation missing or wrong type: %T", openclaw["presentation"])
	}
	suggestedActions, ok := presentation["suggested_actions"].([]any)
	if !ok || len(suggestedActions) != 1 {
		t.Fatalf("openclaw.presentation.suggested_actions = %#v, want len=1", presentation["suggested_actions"])
	}

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) != 1 {
		t.Fatalf("quick_actions = %#v, want len=1", payload["quick_actions"])
	}
	firstAction, ok := quickActions[0].(map[string]any)
	if !ok {
		t.Fatalf("quick_actions[0] wrong type: %T", quickActions[0])
	}
	if got, _ := firstAction["action_id"].(string); got != "status" {
		t.Fatalf("quick_actions[0].action_id = %q, want status", got)
	}

	actionMap, ok := payload["quick_action_by_id"].(map[string]any)
	if !ok {
		t.Fatalf("quick_action_by_id missing or wrong type: %T", payload["quick_action_by_id"])
	}
	if got, _ := actionMap["status"].(string); got != "amux --json status" {
		t.Fatalf("quick_action_by_id[status] = %q, want %q", got, "amux --json status")
	}
}

func TestOpenClawStepWrapper_UsesChannelAndWrapperSuggestions(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-step.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
if [[ "${1:-}" == "agent" && "${2:-}" == "run" ]]; then
  printf '%s' "${FAKE_AMUX_RUN_JSON:?missing FAKE_AMUX_RUN_JSON}"
  exit 0
fi
echo "unexpected args: $*" >&2
exit 2
`)

	runJSON := `{"ok":true,"data":{"session_name":"sess-wrap-1","agent_id":"agent-wrap-1","workspace_id":"ws-wrap-1","assistant":"codex","response":{"status":"timed_out","latest_line":"Still running build","summary":"Timed out; build still running.","delta":"Still running build","needs_input":false,"input_hint":"","timed_out":true,"session_exited":false,"changed":true}}}`
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "FAKE_AMUX_RUN_JSON", runJSON)
	env = withEnv(env, "OPENCLAW_CHANNEL", "slack")

	payload := runScriptJSON(t, scriptPath, env,
		"run",
		"--workspace", "ws-wrap-1",
		"--assistant", "codex",
		"--prompt", "continue",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)

	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "openclaw-step.sh send --agent") {
		t.Fatalf("suggested_command = %q, expected openclaw-step wrapper command", suggested)
	}

	openclaw, ok := payload["openclaw"].(map[string]any)
	if !ok {
		t.Fatalf("openclaw missing or wrong type: %T", payload["openclaw"])
	}
	if got, _ := openclaw["selected_channel"].(string); got != "slack" {
		t.Fatalf("openclaw.selected_channel = %q, want slack", got)
	}
}

func TestOpenClawDXWrapper_UsesChannelAndWrapperSuggestions(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[{"name":"api","path":"/tmp/api"},{"name":"mobile","path":"/tmp/mobile"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_CHANNEL", "discord")

	payload := runScriptJSON(t, scriptPath, env,
		"project", "list",
		"--query", "api",
	)

	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "openclaw-dx.sh") {
		t.Fatalf("suggested_command = %q, expected openclaw-dx wrapper command", suggested)
	}

	openclaw, ok := payload["openclaw"].(map[string]any)
	if !ok {
		t.Fatalf("openclaw missing or wrong type: %T", payload["openclaw"])
	}
	if got, _ := openclaw["selected_channel"].(string); got != "discord" {
		t.Fatalf("openclaw.selected_channel = %q, want discord", got)
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	firstAction, ok := quickActions[0].(map[string]any)
	if !ok {
		t.Fatalf("quick_actions[0] wrong type: %T", quickActions[0])
	}
	if got, _ := firstAction["action_id"].(string); got == "" {
		t.Fatalf("quick_actions[0].action_id is empty")
	}
}
