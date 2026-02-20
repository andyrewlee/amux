package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawTurnScript_AddsChunkContinuationMetadata(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-turn.sh")
	fakeStepDir := t.TempDir()
	fakeStepPath := filepath.Join(fakeStepDir, "fake-step.sh")
	counterPath := filepath.Join(fakeStepDir, "counter.txt")

	if err := os.WriteFile(fakeStepPath, []byte(`#!/usr/bin/env bash
set -euo pipefail
count_file="${FAKE_STEP_COUNT_FILE:?missing FAKE_STEP_COUNT_FILE}"
count=0
if [[ -f "$count_file" ]]; then
  count="$(cat "$count_file")"
fi
count=$((count + 1))
printf '%s' "$count" > "$count_file"
if [[ "$count" -eq 1 ]]; then
  printf '%s' "${FAKE_STEP_1_JSON:?missing FAKE_STEP_1_JSON}"
else
  printf '%s' "${FAKE_STEP_2_JSON:?missing FAKE_STEP_2_JSON}"
fi
`), 0o755); err != nil {
		t.Fatalf("write fake step script: %v", err)
	}

	step1 := `{"ok":true,"mode":"run","status":"timed_out","summary":"Warming up and gathering repository context.","agent_id":"agent-3","workspace_id":"ws-3","assistant":"codex","response":{"substantive_output":false,"needs_input":false},"next_action":"Continue.","suggested_command":"skills/amux/scripts/openclaw-step.sh send --agent agent-3 --text \"Continue\" --enter --wait-timeout 60s --idle-threshold 10s"}`
	step2 := `{"ok":true,"mode":"send","status":"idle","summary":"Implemented a long list of refactors and added tests and docs and validation so the final OpenClaw update should be split into multiple chunks for readability.","agent_id":"agent-3","workspace_id":"ws-3","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Review patch.","suggested_command":""}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-3",
		"--assistant", "codex",
		"--prompt", "Large update",
		"--max-steps", "3",
		"--turn-budget", "120",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	env := os.Environ()
	env = withEnv(env, "FAKE_STEP_COUNT_FILE", counterPath)
	env = withEnv(env, "FAKE_STEP_1_JSON", step1)
	env = withEnv(env, "FAKE_STEP_2_JSON", step2)
	env = withEnv(env, "OPENCLAW_TURN_STEP_SCRIPT", fakeStepPath)
	env = withEnv(env, "OPENCLAW_TURN_CHUNK_CHARS", "80")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("openclaw-turn.sh run failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}

	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	chunks, ok := channel["chunks"].([]any)
	if !ok || len(chunks) < 2 {
		t.Fatalf("openclaw.chunks expected at least 2 chunks: %#v", channel["chunks"])
	}
	secondChunk, _ := chunks[1].(string)
	if !strings.HasPrefix(secondChunk, "continued (2/") {
		t.Fatalf("second chunk missing continuation prefix: %q", secondChunk)
	}
}

func TestOpenClawTurnScript_UsesSiblingStepScriptWhenInvokedOutsideRepoRoot(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	sourceScriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-turn.sh")
	src, err := os.ReadFile(sourceScriptPath)
	if err != nil {
		t.Fatalf("read source script: %v", err)
	}

	scriptDir := t.TempDir()
	runDir := t.TempDir()
	copiedScriptPath := filepath.Join(scriptDir, "openclaw-turn.sh")
	if err := os.WriteFile(copiedScriptPath, src, 0o755); err != nil {
		t.Fatalf("write copied script: %v", err)
	}

	argsLog := filepath.Join(scriptDir, "step-args.log")
	fakeStepPath := filepath.Join(scriptDir, "openclaw-step.sh")
	if err := os.WriteFile(fakeStepPath, []byte(`#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" > "${STEP_ARGS_LOG:?missing STEP_ARGS_LOG}"
printf '%s' '{"ok":true,"mode":"run","status":"idle","summary":"Sibling step used.","agent_id":"agent-sib","workspace_id":"ws-sib","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"","suggested_command":""}'
`), 0o755); err != nil {
		t.Fatalf("write fake step script: %v", err)
	}

	cmd := exec.Command(
		copiedScriptPath,
		"run",
		"--workspace", "ws-sib",
		"--assistant", "codex",
		"--prompt", "test prompt",
		"--max-steps", "1",
		"--turn-budget", "30",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	cmd.Dir = runDir
	env := os.Environ()
	env = withEnv(env, "STEP_ARGS_LOG", argsLog)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("openclaw-turn.sh run failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}
	if got, _ := payload["status"].(string); got != "idle" {
		t.Fatalf("status = %q, want %q", got, "idle")
	}
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want %q", got, "completed")
	}

	argsRaw, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("read step args: %v", err)
	}
	args := strings.TrimSpace(string(argsRaw))
	if !strings.Contains(args, "run") || !strings.Contains(args, "--workspace ws-sib") {
		t.Fatalf("step args = %q, expected run/workspace flags", args)
	}

	quickActionMap, ok := payload["quick_action_map"].(map[string]any)
	if !ok {
		t.Fatalf("quick_action_map missing or wrong type: %T", payload["quick_action_map"])
	}
	statusCmd, _ := quickActionMap["qa:status"].(string)
	if !strings.HasPrefix(statusCmd, "skills/amux/scripts/openclaw-step.sh send --agent agent-sib") {
		t.Fatalf("status quick action = %q, expected openclaw-step command", statusCmd)
	}
}
