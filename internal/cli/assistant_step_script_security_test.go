package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistantStepScriptRun_RedactsSecretsInOutput(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-step.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	if err := os.WriteFile(fakeAmuxPath, []byte(`#!/usr/bin/env bash
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
`), 0o755); err != nil {
		t.Fatalf("write fake amux: %v", err)
	}

	runJSON := `{"ok":true,"data":{"session_name":"sess-secret","agent_id":"agent-secret","workspace_id":"ws-secret","assistant":"codex","response":{"status":"idle","latest_line":"token=ghp_abcde1234567890","summary":"Use token sk-ant-api1-abcdefghijklmnopqrstuv in env","delta":"Authorization: Bearer sk-ant-api1-abcdefghijklmnopqrstuv123456\nSECRET=supersecretvalue123456","needs_input":false,"input_hint":"","timed_out":false,"session_exited":false,"changed":true}}}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-secret",
		"--assistant", "codex",
		"--prompt", "test prompt",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "FAKE_AMUX_RUN_JSON", runJSON)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("assistant-step.sh run failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}

	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "ghp_abcde1234567890") || strings.Contains(summary, "sk-ant-api1-abcdefghijklmnopqrstuv123456") {
		t.Fatalf("summary leaked secret: %q", summary)
	}
	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	msg, _ := channel["message"].(string)
	if strings.Contains(msg, "Bearer sk-ant-api1-abcdefghijklmnopqrstuv123456") || strings.Contains(msg, "SECRET=supersecretvalue123456") {
		t.Fatalf("assistant.message leaked secret: %q", msg)
	}
}
