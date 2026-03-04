package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistantTurnScriptSend_AllowsEnterWithoutText(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-turn.sh")
	fakeStepDir := t.TempDir()
	fakeStepPath := filepath.Join(fakeStepDir, "fake-step.sh")
	argsLog := filepath.Join(fakeStepDir, "step-args.log")
	if err := os.WriteFile(fakeStepPath, []byte(`#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" > "${FAKE_STEP_ARGS_LOG:?missing FAKE_STEP_ARGS_LOG}"
printf '%s' '{"ok":true,"mode":"send","status":"idle","summary":"done","agent_id":"agent-1","workspace_id":"ws-1","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Review changes.","suggested_command":""}'
`), 0o755); err != nil {
		t.Fatalf("write fake step script: %v", err)
	}

	cmd := exec.Command(
		scriptPath,
		"send",
		"--agent", "agent-1",
		"--enter",
		"--max-steps", "1",
		"--turn-budget", "30",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	env := os.Environ()
	env = withEnv(env, "AMUX_ASSISTANT_TURN_STEP_SCRIPT", fakeStepPath)
	env = withEnv(env, "FAKE_STEP_ARGS_LOG", argsLog)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("assistant-turn.sh send failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}
	if got, _ := payload["status"].(string); got == "command_error" {
		t.Fatalf("status = %q, want non-command_error for enter-only send", got)
	}

	argsRaw, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("read step args: %v", err)
	}
	args := strings.TrimSpace(string(argsRaw))
	if !strings.Contains(args, "send --agent agent-1") || !strings.Contains(args, "--enter") {
		t.Fatalf("step args = %q, expected send/agent/enter", args)
	}
	if strings.Contains(args, "--text") {
		t.Fatalf("step args = %q, expected no --text for enter-only send", args)
	}
}
