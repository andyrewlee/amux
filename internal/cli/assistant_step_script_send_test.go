package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAssistantStepScriptSend_AllowsEnterWithoutText(t *testing.T) {
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
if [[ "${1:-}" == "agent" && "${2:-}" == "send" ]]; then
  printf '%s' '{"ok":true,"data":{"session_name":"sess-enter","agent_id":"agent-enter","workspace_id":"ws-enter","assistant":"codex","response":{"status":"idle","latest_line":"continued","summary":"continued","delta":"continued","needs_input":false,"input_hint":"","timed_out":false,"session_exited":false,"changed":true}}}'
  exit 0
fi
echo "unexpected args: $*" >&2
exit 2
`), 0o755); err != nil {
		t.Fatalf("write fake amux: %v", err)
	}

	cmd := exec.Command(
		scriptPath,
		"send",
		"--agent", "agent-enter",
		"--enter",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("assistant-step.sh send failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}
	if got, _ := payload["status"].(string); got != "idle" {
		t.Fatalf("status = %q, want %q", got, "idle")
	}
	if got, _ := payload["ok"].(bool); !got {
		t.Fatalf("ok = %v, want true", got)
	}
}
