package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawTurnScript_CompletesAfterFollowupStep(t *testing.T) {
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
case "$count" in
  1) printf '%s' "${FAKE_STEP_1_JSON:?missing FAKE_STEP_1_JSON}" ;;
  *) printf '%s' "${FAKE_STEP_2_JSON:?missing FAKE_STEP_2_JSON}" ;;
esac
`), 0o755); err != nil {
		t.Fatalf("write fake step script: %v", err)
	}

	step1 := `{"ok":true,"mode":"run","status":"timed_out","summary":"Timed out waiting for first visible output; agent may still be starting.","agent_id":"agent-1","workspace_id":"ws-1","assistant":"codex","response":{"substantive_output":false,"needs_input":false},"next_action":"Run one focused follow-up step.","suggested_command":"skills/amux/scripts/openclaw-step.sh send --agent agent-1 --text \"Continue\" --enter --wait-timeout 60s --idle-threshold 10s"}`
	step2 := `{"ok":true,"mode":"send","status":"idle","summary":"Refactor applied and tests passed.","agent_id":"agent-1","workspace_id":"ws-1","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Review uncommitted changes.","suggested_command":""}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-1",
		"--assistant", "codex",
		"--prompt", "Improve debt hotspots",
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
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("openclaw-turn.sh run failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}

	if got, _ := payload["ok"].(bool); !got {
		t.Fatalf("ok = %v, want true", got)
	}
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want %q", got, "completed")
	}
	if got, _ := payload["status"].(string); got != "idle" {
		t.Fatalf("status = %q, want %q", got, "idle")
	}
	if got, _ := payload["verbosity"].(string); got != "normal" {
		t.Fatalf("verbosity = %q, want %q", got, "normal")
	}
	if got, _ := payload["steps_used"].(float64); got != 2 {
		t.Fatalf("steps_used = %v, want 2", got)
	}
	if got, _ := payload["progress_percent"].(float64); got != 66 {
		t.Fatalf("progress_percent = %v, want 66", got)
	}
	events, ok := payload["events"].([]any)
	if !ok || len(events) != 2 {
		t.Fatalf("events = %#v, want len=2", payload["events"])
	}
	milestones, ok := payload["milestones"].([]any)
	if !ok || len(milestones) != 2 {
		t.Fatalf("milestones = %#v, want len=2", payload["milestones"])
	}
	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	chunks, ok := channel["chunks"].([]any)
	if !ok || len(chunks) == 0 {
		t.Fatalf("openclaw.chunks missing or empty: %#v", channel["chunks"])
	}
	chunksMeta, ok := channel["chunks_meta"].([]any)
	if !ok || len(chunksMeta) == 0 {
		t.Fatalf("openclaw.chunks_meta missing or empty: %#v", channel["chunks_meta"])
	}
	progressUpdates, ok := payload["progress_updates"].([]any)
	if !ok || len(progressUpdates) != 2 {
		t.Fatalf("progress_updates = %#v, want len=2", payload["progress_updates"])
	}
	delivery, ok := payload["delivery"].(map[string]any)
	if !ok {
		t.Fatalf("delivery missing or wrong type: %T", payload["delivery"])
	}
	if got, _ := delivery["action"].(string); got != "send" {
		t.Fatalf("delivery.action = %q, want %q", got, "send")
	}
	if got, _ := delivery["drop_pending"].(bool); !got {
		t.Fatalf("delivery.drop_pending = %v, want true", got)
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	quickActionMap, ok := payload["quick_action_map"].(map[string]any)
	if !ok || len(quickActionMap) == 0 {
		t.Fatalf("quick_action_map missing or empty: %#v", payload["quick_action_map"])
	}
	quickActionPrompts, ok := payload["quick_action_prompts"].(map[string]any)
	if !ok || len(quickActionPrompts) == 0 {
		t.Fatalf("quick_action_prompts missing or empty: %#v", payload["quick_action_prompts"])
	}
	for _, actionRaw := range quickActions {
		action, ok := actionRaw.(map[string]any)
		if !ok {
			t.Fatalf("quick action has wrong type: %T", actionRaw)
		}
		callbackData, _ := action["callback_data"].(string)
		if !strings.HasPrefix(callbackData, "qa:") {
			t.Fatalf("callback_data = %q, want qa: prefix", callbackData)
		}
		if len(callbackData) > 64 {
			t.Fatalf("callback_data too long (%d): %q", len(callbackData), callbackData)
		}
	}
	channelProgressUpdates, ok := channel["progress_updates"].([]any)
	if !ok || len(channelProgressUpdates) != 2 {
		t.Fatalf("openclaw.progress_updates = %#v, want len=2", channel["progress_updates"])
	}
	if got, _ := channel["verbosity"].(string); got != "normal" {
		t.Fatalf("openclaw.verbosity = %q, want %q", got, "normal")
	}
	if got, _ := channel["inline_buttons_scope"].(string); got != "allowlist" {
		t.Fatalf("openclaw.inline_buttons_scope = %q, want allowlist", got)
	}
	if got, _ := channel["inline_buttons_enabled"].(bool); !got {
		t.Fatalf("openclaw.inline_buttons_enabled = %v, want true", got)
	}
	if got, _ := channel["callback_data_max_bytes"].(float64); got != 64 {
		t.Fatalf("openclaw.callback_data_max_bytes = %v, want 64", got)
	}
	inlineButtons, ok := channel["inline_buttons"].([]any)
	if !ok || len(inlineButtons) == 0 {
		t.Fatalf("openclaw.inline_buttons missing or empty: %#v", channel["inline_buttons"])
	}
	actionTokens, ok := channel["action_tokens"].([]any)
	if !ok || len(actionTokens) == 0 {
		t.Fatalf("openclaw.action_tokens missing or empty: %#v", channel["action_tokens"])
	}
	actionsFallback, _ := channel["actions_fallback"].(string)
	if !strings.Contains(actionsFallback, "qa:") {
		t.Fatalf("openclaw.actions_fallback = %q, expected qa: token list", actionsFallback)
	}
}

func TestOpenClawTurnScript_CoalescesDuplicateMilestonesAndStopsOnTimeoutStreak(t *testing.T) {
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

	step1 := `{"ok":true,"mode":"run","status":"timed_out","summary":"Agent warming up.","agent_id":"agent-2","workspace_id":"ws-2","assistant":"codex","response":{"substantive_output":false,"needs_input":false},"next_action":"Retry with a short follow-up.","suggested_command":"skills/amux/scripts/openclaw-step.sh send --agent agent-2 --text \"Continue\" --enter --wait-timeout 60s --idle-threshold 10s"}`
	step2 := `{"ok":true,"mode":"send","status":"timed_out","summary":"Agent warming up.","agent_id":"agent-2","workspace_id":"ws-2","assistant":"codex","response":{"substantive_output":false,"needs_input":false},"next_action":"Retry with a short follow-up.","suggested_command":"skills/amux/scripts/openclaw-step.sh send --agent agent-2 --text \"Continue\" --enter --wait-timeout 60s --idle-threshold 10s"}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-2",
		"--assistant", "codex",
		"--prompt", "Investigate progress",
		"--max-steps", "4",
		"--turn-budget", "120",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	env := os.Environ()
	env = withEnv(env, "FAKE_STEP_COUNT_FILE", counterPath)
	env = withEnv(env, "FAKE_STEP_1_JSON", step1)
	env = withEnv(env, "FAKE_STEP_2_JSON", step2)
	env = withEnv(env, "OPENCLAW_TURN_STEP_SCRIPT", fakeStepPath)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("openclaw-turn.sh run failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}

	if got, _ := payload["overall_status"].(string); got != "timed_out" {
		t.Fatalf("overall_status = %q, want %q", got, "timed_out")
	}
	if got, _ := payload["steps_used"].(float64); got != 2 {
		t.Fatalf("steps_used = %v, want 2", got)
	}
	if got, _ := payload["progress_percent"].(float64); got != 50 {
		t.Fatalf("progress_percent = %v, want 50", got)
	}
	if got, _ := payload["timeout_streak"].(float64); got != 2 {
		t.Fatalf("timeout_streak = %v, want 2", got)
	}
	if got, _ := payload["budget_exhausted"].(bool); got {
		t.Fatalf("budget_exhausted = true, want false")
	}
	events, ok := payload["events"].([]any)
	if !ok || len(events) != 2 {
		t.Fatalf("events = %#v, want len=2", payload["events"])
	}
	milestones, ok := payload["milestones"].([]any)
	if !ok || len(milestones) != 1 {
		t.Fatalf("milestones = %#v, want len=1 (coalesced)", payload["milestones"])
	}
	delivery, ok := payload["delivery"].(map[string]any)
	if !ok {
		t.Fatalf("delivery missing or wrong type: %T", payload["delivery"])
	}
	if got, _ := delivery["action"].(string); got != "edit" {
		t.Fatalf("delivery.action = %q, want %q", got, "edit")
	}
	if got, _ := delivery["replace_previous"].(bool); !got {
		t.Fatalf("delivery.replace_previous = %v, want true", got)
	}
	progressUpdates, ok := payload["progress_updates"].([]any)
	if !ok || len(progressUpdates) != 1 {
		t.Fatalf("progress_updates = %#v, want len=1", payload["progress_updates"])
	}
}

func TestOpenClawTurnScript_QuietVerbositySuppressesExtraSections(t *testing.T) {
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
printf '%s' "${FAKE_STEP_1_JSON:?missing FAKE_STEP_1_JSON}"
`), 0o755); err != nil {
		t.Fatalf("write fake step script: %v", err)
	}

	step1 := `{"ok":true,"mode":"run","status":"needs_input","summary":"Need approval to continue.","agent_id":"agent-q","workspace_id":"ws-q","assistant":"codex","response":{"substantive_output":true,"needs_input":true},"next_action":"Choose A or B.","suggested_command":"skills/amux/scripts/openclaw-step.sh send --agent agent-q --text \"A\" --enter --wait-timeout 60s --idle-threshold 10s"}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-q",
		"--assistant", "codex",
		"--prompt", "Need input",
		"--max-steps", "2",
		"--turn-budget", "120",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	env := os.Environ()
	env = withEnv(env, "FAKE_STEP_COUNT_FILE", counterPath)
	env = withEnv(env, "FAKE_STEP_1_JSON", step1)
	env = withEnv(env, "OPENCLAW_TURN_STEP_SCRIPT", fakeStepPath)
	env = withEnv(env, "OPENCLAW_TURN_VERBOSITY", "quiet")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("openclaw-turn.sh run failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}

	if got, _ := payload["verbosity"].(string); got != "quiet" {
		t.Fatalf("verbosity = %q, want quiet", got)
	}
	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	msg, _ := channel["message"].(string)
	if strings.Contains(msg, "Next:") || strings.Contains(msg, "Command:") || strings.Contains(msg, "Meta:") || strings.Contains(msg, "Progress:") {
		t.Fatalf("openclaw.message should suppress extra sections in quiet mode: %q", msg)
	}
}

func TestOpenClawTurnScript_DisablesInlineButtonsWhenScopeOff(t *testing.T) {
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
printf '%s' "${FAKE_STEP_1_JSON:?missing FAKE_STEP_1_JSON}"
`), 0o755); err != nil {
		t.Fatalf("write fake step script: %v", err)
	}

	step1 := `{"ok":true,"mode":"run","status":"needs_input","summary":"Need approval to continue.","agent_id":"agent-inline-off","workspace_id":"ws-inline-off","assistant":"codex","response":{"substantive_output":true,"needs_input":true},"next_action":"Choose A or B.","suggested_command":"skills/amux/scripts/openclaw-step.sh send --agent agent-inline-off --text \"A\" --enter --wait-timeout 60s --idle-threshold 10s"}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-inline-off",
		"--assistant", "codex",
		"--prompt", "Need input",
		"--max-steps", "2",
		"--turn-budget", "120",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	env := os.Environ()
	env = withEnv(env, "FAKE_STEP_COUNT_FILE", counterPath)
	env = withEnv(env, "FAKE_STEP_1_JSON", step1)
	env = withEnv(env, "OPENCLAW_TURN_STEP_SCRIPT", fakeStepPath)
	env = withEnv(env, "OPENCLAW_INLINE_BUTTONS_SCOPE", "off")
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
	if got, _ := channel["inline_buttons_scope"].(string); got != "off" {
		t.Fatalf("openclaw.inline_buttons_scope = %q, want off", got)
	}
	if got, _ := channel["inline_buttons_enabled"].(bool); got {
		t.Fatalf("openclaw.inline_buttons_enabled = %v, want false", got)
	}
	if inlineButtons, ok := channel["inline_buttons"].([]any); !ok || len(inlineButtons) != 0 {
		t.Fatalf("openclaw.inline_buttons = %#v, want empty", channel["inline_buttons"])
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
}
