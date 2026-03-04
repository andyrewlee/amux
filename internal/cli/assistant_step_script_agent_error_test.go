package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAssistantStepScriptRun_AgentErrorEnvelopeUsesMessageAsSummary(t *testing.T) {
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
  exit "${FAKE_AMUX_EXIT_CODE:-1}"
fi
echo "unexpected args: $*" >&2
exit 2
`), 0o755); err != nil {
		t.Fatalf("write fake amux: %v", err)
	}

	runJSON := `{"ok":false,"error":{"code":"unknown_assistant","message":"unknown assistant: codxe"}}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-err",
		"--assistant", "codex",
		"--prompt", "test prompt",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "FAKE_AMUX_RUN_JSON", runJSON)
	env = withEnv(env, "FAKE_AMUX_EXIT_CODE", "1")
	cmd.Env = env
	out, _ := cmd.CombinedOutput()

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}

	if got, _ := payload["status"].(string); got != "agent_error" {
		t.Fatalf("status = %q, want %q", got, "agent_error")
	}
	if got, _ := payload["summary"].(string); got != "unknown assistant: codxe" {
		t.Fatalf("summary = %q, want %q", got, "unknown assistant: codxe")
	}
	if got, _ := payload["error"].(string); got != "unknown_assistant" {
		t.Fatalf("error = %q, want %q", got, "unknown_assistant")
	}
}

func TestAssistantStepScriptRun_AgentErrorEnvelopeUsesMessageAsSummaryWhenExitZero(t *testing.T) {
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

	runJSON := `{"ok":false,"error":{"code":"session_not_found","message":"session not found"}}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-err-zero",
		"--assistant", "codex",
		"--prompt", "test prompt",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "FAKE_AMUX_RUN_JSON", runJSON)
	cmd.Env = env
	out, _ := cmd.CombinedOutput()

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}

	if got, _ := payload["status"].(string); got != "agent_error" {
		t.Fatalf("status = %q, want %q", got, "agent_error")
	}
	if got, _ := payload["summary"].(string); got != "session not found" {
		t.Fatalf("summary = %q, want %q", got, "session not found")
	}
	if got, _ := payload["error"].(string); got != "session_not_found" {
		t.Fatalf("error = %q, want %q", got, "session_not_found")
	}
}
