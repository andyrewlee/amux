package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawStepScriptRun_QuietVerbositySuppressesDetailSections(t *testing.T) {
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

	runJSON := `{"ok":true,"data":{"session_name":"sess-quiet","agent_id":"agent-quiet","workspace_id":"ws-quiet","assistant":"codex","response":{"status":"idle","latest_line":"done","summary":"done","delta":"line1\nline2\nline3\nline4","needs_input":false,"input_hint":"","timed_out":false,"session_exited":false,"changed":true}}}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-quiet",
		"--assistant", "codex",
		"--prompt", "test prompt",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "FAKE_AMUX_RUN_JSON", runJSON)
	env = withEnv(env, "OPENCLAW_STEP_VERBOSITY", "quiet")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("openclaw-step.sh run failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}

	if got, _ := payload["verbosity"].(string); got != "quiet" {
		t.Fatalf("verbosity = %q, want %q", got, "quiet")
	}
	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	msg, _ := channel["message"].(string)
	if strings.Contains(msg, "Details:") || strings.Contains(msg, "Command:") || strings.Contains(msg, "Next:") {
		t.Fatalf("openclaw.message should suppress detail sections in quiet mode: %q", msg)
	}
}

func TestOpenClawStepScriptRun_DisablesInlineButtonsWhenScopeOff(t *testing.T) {
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

	runJSON := `{"ok":true,"data":{"session_name":"sess-inline-off","agent_id":"agent-inline-off","workspace_id":"ws-inline-off","assistant":"codex","response":{"status":"idle","latest_line":"done","summary":"done","delta":"line1\nline2","needs_input":false,"input_hint":"","timed_out":false,"session_exited":false,"changed":true}}}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-inline-off",
		"--assistant", "codex",
		"--prompt", "test prompt",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "FAKE_AMUX_RUN_JSON", runJSON)
	env = withEnv(env, "OPENCLAW_INLINE_BUTTONS_SCOPE", "off")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("openclaw-step.sh run failed: %v", err)
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

func TestOpenClawStepScriptRun_AddsChunkContinuationMetadata(t *testing.T) {
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

	runJSON := `{"ok":true,"data":{"session_name":"sess-4","agent_id":"agent-4","workspace_id":"ws-4","assistant":"codex","response":{"status":"idle","latest_line":"done","summary":"This is a very long summary intended to force OpenClaw chunking into multiple segments with continuation metadata for better mobile readability.","delta":"line1\nline2\nline3\nline4\nline5\nline6","needs_input":false,"input_hint":"","timed_out":false,"session_exited":false,"changed":true}}}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-4",
		"--assistant", "codex",
		"--prompt", "test prompt",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "FAKE_AMUX_RUN_JSON", runJSON)
	env = withEnv(env, "OPENCLAW_STEP_CHUNK_CHARS", "80")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("openclaw-step.sh run failed: %v", err)
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
	chunksMeta, ok := channel["chunks_meta"].([]any)
	if !ok || len(chunksMeta) != len(chunks) {
		t.Fatalf("openclaw.chunks_meta mismatch: chunks=%d meta=%#v", len(chunks), channel["chunks_meta"])
	}
}

func TestOpenClawStepScriptRun_UsesAbsoluteSelfPathInSuggestedCommand(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	relScriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-step.sh")
	scriptPath, err := filepath.Abs(relScriptPath)
	if err != nil {
		t.Fatalf("abs script path: %v", err)
	}

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

	runJSON := `{"ok":true,"data":{"session_name":"sess-abs","agent_id":"agent-abs","workspace_id":"ws-abs","assistant":"codex","response":{"status":"timed_out","latest_line":"","summary":"","delta":"","needs_input":false,"input_hint":"","timed_out":true,"session_exited":false,"changed":false}}}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-abs",
		"--assistant", "codex",
		"--prompt", "test prompt",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	cmd.Dir = t.TempDir()
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "FAKE_AMUX_RUN_JSON", runJSON)
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
	suggested, _ := payload["suggested_command"].(string)
	if !strings.HasPrefix(suggested, "skills/amux/scripts/openclaw-step.sh send --agent agent-abs") {
		t.Fatalf("suggested_command = %q, want openclaw-step command prefix", suggested)
	}
}
