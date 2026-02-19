package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func withEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		out = append(out, item)
	}
	return append(out, prefix+value)
}

func requireBinary(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available in PATH", name)
	}
}

func TestOpenClawStepScriptRun_RecoversTimedOutNoOutputFromCapture(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-step.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	if err := os.WriteFile(fakeAmuxPath, []byte(`#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "agent run")
    printf '%s' "${FAKE_AMUX_RUN_JSON:?missing FAKE_AMUX_RUN_JSON}"
    ;;
  "agent send")
    printf '%s' "${FAKE_AMUX_SEND_JSON:-${FAKE_AMUX_RUN_JSON:?missing FAKE_AMUX_SEND_JSON}}"
    ;;
  "agent capture")
    printf '%s' "${FAKE_AMUX_CAPTURE_JSON:?missing FAKE_AMUX_CAPTURE_JSON}"
    ;;
  *)
    echo "unexpected args: $*" >&2
    exit 2
    ;;
esac
`), 0o755); err != nil {
		t.Fatalf("write fake amux: %v", err)
	}

	runJSON := `{"ok":true,"data":{"session_name":"sess-1","agent_id":"agent-1","workspace_id":"ws-1","assistant":"codex","response":{"status":"timed_out","latest_line":"(no output yet)","summary":"(no output yet)","delta":"","needs_input":false,"input_hint":"","timed_out":true,"session_exited":false,"changed":false}}}`
	captureJSON := `{"ok":true,"data":{"content":"\u001b[2m? for shortcuts\u001b[0m\n• Recovered status update"}}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-1",
		"--assistant", "codex",
		"--prompt", "test prompt",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "FAKE_AMUX_RUN_JSON", runJSON)
	env = withEnv(env, "FAKE_AMUX_CAPTURE_JSON", captureJSON)
	env = withEnv(env, "OPENCLAW_STEP_TIMEOUT_RECOVERY_POLLS", "1")
	env = withEnv(env, "OPENCLAW_STEP_TIMEOUT_RECOVERY_INTERVAL", "0")
	env = withEnv(env, "OPENCLAW_STEP_TIMEOUT_RECOVERY_LINES", "80")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("openclaw-step.sh run failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}

	if got, _ := payload["ok"].(bool); !got {
		t.Fatalf("ok = %v, want true", got)
	}
	if got, _ := payload["status"].(string); got != "timed_out" {
		t.Fatalf("status = %q, want %q", got, "timed_out")
	}
	if got, _ := payload["verbosity"].(string); got != "normal" {
		t.Fatalf("verbosity = %q, want %q", got, "normal")
	}
	if got, _ := payload["status_emoji"].(string); got != "⏱️" {
		t.Fatalf("status_emoji = %q, want %q", got, "⏱️")
	}
	if got, _ := payload["recovered_from_capture"].(bool); !got {
		t.Fatalf("recovered_from_capture = %v, want true", got)
	}
	idempotencyKey, _ := payload["idempotency_key"].(string)
	if !strings.HasPrefix(idempotencyKey, "tgstep-") {
		t.Fatalf("idempotency_key = %q, want prefix tgstep-", idempotencyKey)
	}
	if got, _ := payload["summary"].(string); got != "• Recovered status update" {
		t.Fatalf("summary = %q", got)
	}
	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	if chunkChars, _ := channel["chunk_chars"].(float64); chunkChars != 1200 {
		t.Fatalf("openclaw.chunk_chars = %v, want 1200", chunkChars)
	}
	msg, _ := channel["message"].(string)
	if !strings.Contains(msg, "Recovered status update") {
		t.Fatalf("openclaw.message = %q, expected summary text", msg)
	}
	if got, _ := channel["verbosity"].(string); got != "normal" {
		t.Fatalf("openclaw.verbosity = %q, want %q", got, "normal")
	}
	if got, _ := channel["inline_buttons_scope"].(string); got != "allowlist" {
		t.Fatalf("openclaw.inline_buttons_scope = %q, want %q", got, "allowlist")
	}
	if got, _ := channel["inline_buttons_enabled"].(bool); !got {
		t.Fatalf("openclaw.inline_buttons_enabled = %v, want true", got)
	}
	if got, _ := channel["callback_data_max_bytes"].(float64); got != 64 {
		t.Fatalf("openclaw.callback_data_max_bytes = %v, want 64", got)
	}
	chunks, ok := channel["chunks"].([]any)
	if !ok || len(chunks) == 0 {
		t.Fatalf("openclaw.chunks missing or empty: %#v", channel["chunks"])
	}
	chunksMeta, ok := channel["chunks_meta"].([]any)
	if !ok || len(chunksMeta) == 0 {
		t.Fatalf("openclaw.chunks_meta missing or empty: %#v", channel["chunks_meta"])
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
	delivery, ok := payload["delivery"].(map[string]any)
	if !ok {
		t.Fatalf("delivery missing or wrong type: %T", payload["delivery"])
	}
	if got, _ := delivery["action"].(string); got != "edit" {
		t.Fatalf("delivery.action = %q, want %q", got, "edit")
	}
	if got, _ := delivery["coalesce"].(bool); !got {
		t.Fatalf("delivery.coalesce = %v, want true", got)
	}
	if got, _ := delivery["replace_previous"].(bool); !got {
		t.Fatalf("delivery.replace_previous = %v, want true", got)
	}

	resp, ok := payload["response"].(map[string]any)
	if !ok {
		t.Fatalf("response missing or wrong type: %T", payload["response"])
	}
	if got, _ := resp["delta_compact"].(string); got != "• Recovered status update" {
		t.Fatalf("delta_compact = %q", got)
	}
	recovery, ok := payload["recovery"].(map[string]any)
	if !ok {
		t.Fatalf("recovery missing or wrong type: %T", payload["recovery"])
	}
	if got, _ := recovery["attempted"].(bool); !got {
		t.Fatalf("recovery.attempted = %v, want true", got)
	}
	if got, _ := recovery["polls_used"].(float64); got != 1 {
		t.Fatalf("recovery.polls_used = %v, want 1", got)
	}
}

func TestOpenClawStepScriptRun_SetsBlockedPermissionMode(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-step.sh")
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

	runJSON := `{"ok":true,"data":{"session_name":"sess-2","agent_id":"agent-2","workspace_id":"ws-2","assistant":"claude","response":{"status":"needs_input","latest_line":"Assistant is waiting for local permission-mode selection.","summary":"Needs input: Assistant is waiting for local permission-mode selection.","delta":"Assistant is waiting for local permission-mode selection.","needs_input":true,"input_hint":"Assistant is waiting for local permission-mode selection.","timed_out":false,"session_exited":false,"changed":true}}}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-2",
		"--assistant", "claude",
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
		t.Fatalf("openclaw-step.sh run failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}

	if got, _ := payload["status"].(string); got != "needs_input" {
		t.Fatalf("status = %q, want %q", got, "needs_input")
	}
	if got, _ := payload["status_emoji"].(string); got != "❓" {
		t.Fatalf("status_emoji = %q, want %q", got, "❓")
	}
	if got, _ := payload["blocked_permission_mode"].(bool); !got {
		t.Fatalf("blocked_permission_mode = %v, want true", got)
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	idempotencyKey, _ := payload["idempotency_key"].(string)
	if !strings.HasPrefix(idempotencyKey, "tgstep-") {
		t.Fatalf("idempotency_key = %q, want prefix tgstep-", idempotencyKey)
	}
	nextAction, _ := payload["next_action"].(string)
	if !strings.Contains(nextAction, "non-interactive assistant") {
		t.Fatalf("next_action = %q, expected non-interactive hint", nextAction)
	}
}

func TestOpenClawStepScriptRun_AutoIdempotencyCanBeDisabled(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-step.sh")
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

	runJSON := `{"ok":true,"data":{"session_name":"sess-3","agent_id":"agent-3","workspace_id":"ws-3","assistant":"codex","response":{"status":"idle","latest_line":"done","summary":"done","delta":"done","needs_input":false,"input_hint":"","timed_out":false,"session_exited":false,"changed":true}}}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-3",
		"--assistant", "codex",
		"--prompt", "test prompt",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "FAKE_AMUX_RUN_JSON", runJSON)
	env = withEnv(env, "OPENCLAW_STEP_AUTO_IDEMPOTENCY", "false")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("openclaw-step.sh run failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}

	if got, _ := payload["idempotency_key"].(string); got != "" {
		t.Fatalf("idempotency_key = %q, want empty when auto idempotency disabled", got)
	}
}

func TestOpenClawStepScriptRun_UpgradesWeakSummaryFromDelta(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-step.sh")
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

	runJSON := `{"ok":true,"data":{"session_name":"sess-weak","agent_id":"agent-weak","workspace_id":"ws-weak","assistant":"codex","response":{"status":"idle","latest_line":"output tracking.","summary":"output tracking.","delta":"Search rg --files\n- internal/cli/cmd_agent_watch.go:207 computeNewLines can duplicate lines when output is rewritten","needs_input":false,"input_hint":"","timed_out":false,"session_exited":false,"changed":true}}}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-weak",
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
		t.Fatalf("openclaw-step.sh run failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}

	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "internal/cli/cmd_agent_watch.go:207") {
		t.Fatalf("summary = %q, expected upgraded delta-based file reference", summary)
	}
}

func TestOpenClawStepScriptRun_RedactsSecretsInOutput(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-step.sh")
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
		t.Fatalf("openclaw-step.sh run failed: %v", err)
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
		t.Fatalf("openclaw.message leaked secret: %q", msg)
	}
}
