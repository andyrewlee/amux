package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistantTurnScript_PropagatesProvidedIdempotencyKeyToSteps(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-turn.sh")
	fakeStepDir := t.TempDir()
	fakeStepPath := filepath.Join(fakeStepDir, "fake-step.sh")
	stepArgsLog := filepath.Join(fakeStepDir, "step-args.log")

	writeExecutable(t, fakeStepPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" > "${STEP_ARGS_LOG:?missing STEP_ARGS_LOG}"
printf '%s' '{"ok":true,"mode":"run","status":"idle","summary":"done","agent_id":"agent-1","workspace_id":"ws-1","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"","suggested_command":"","quick_actions":[],"channel":{"message":"done","chunks":["done"],"chunks_meta":[{"index":1,"total":1,"text":"done"}]}}'
`)

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-1",
		"--assistant", "codex",
		"--prompt", "run review",
		"--idempotency-key", "dx-review-manual",
		"--max-steps", "1",
		"--turn-budget", "30",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	env := os.Environ()
	env = withEnv(env, "AMUX_ASSISTANT_TURN_STEP_SCRIPT", fakeStepPath)
	env = withEnv(env, "STEP_ARGS_LOG", stepArgsLog)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("assistant-turn.sh run failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}
	if got, _ := payload["status"].(string); got != "idle" {
		t.Fatalf("status = %q, want idle", got)
	}

	stepArgsRaw, err := os.ReadFile(stepArgsLog)
	if err != nil {
		t.Fatalf("read step args: %v", err)
	}
	stepArgs := strings.TrimSpace(string(stepArgsRaw))
	if !strings.Contains(stepArgs, "--idempotency-key dx-review-manual-step-1") {
		t.Fatalf("step args = %q, expected propagated step idempotency key", stepArgs)
	}
}
