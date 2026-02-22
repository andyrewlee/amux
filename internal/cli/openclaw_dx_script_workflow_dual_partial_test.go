package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXWorkflowDual_ReviewPartialDefaultsToReviewCommand(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"demo","repo":"/tmp/demo"}],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":{},"error":null}'
    ;;
esac
`)

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
assistant=""
for ((i=1; i<=$#; i++)); do
  if [[ "${!i}" == "--assistant" ]]; then
    next=$((i+1))
    assistant="${!next}"
  fi
done
if [[ "$assistant" == "claude" ]]; then
  printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"Implementation completed.","agent_id":"agent-impl","workspace_id":"ws-1","assistant":"claude","next_action":"Run review.","suggested_command":"skills/amux/scripts/openclaw-dx.sh git ship --workspace ws-1","quick_actions":[],"channel":{"message":"impl done","chunks":["impl done"],"chunks_meta":[{"index":1,"total":1,"text":"impl done"}],"inline_buttons":[]}}'
  exit 0
fi
printf '%s' '{"ok":true,"mode":"run","status":"partial","overall_status":"partial","summary":"Review partial progress.","agent_id":"agent-review","workspace_id":"ws-1","assistant":"codex","next_action":"Continue review.","suggested_command":"","quick_actions":[],"channel":{"message":"review partial","chunks":["review partial"],"chunks_meta":[{"index":1,"total":1,"text":"review partial"}],"inline_buttons":[]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "OPENCLAW_DX_SELF_SCRIPT", scriptPath)
	env = withEnv(env, "OPENCLAW_PRESENT_SCRIPT", "/nonexistent")

	payload := runScriptJSON(t, scriptPath, env,
		"workflow", "dual",
		"--workspace", "ws-1",
		"--implement-assistant", "claude",
		"--review-assistant", "codex",
	)

	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "review --workspace ws-1 --assistant codex") && !strings.Contains(suggested, "continue --agent agent-review") && !strings.Contains(suggested, "status --workspace ws-1") {
		t.Fatalf("suggested_command = %q, want review-follow-up command", suggested)
	}
	if strings.Contains(suggested, "git ship --workspace ws-1") {
		t.Fatalf("suggested_command = %q, should not suggest ship on partial review", suggested)
	}
}

func TestOpenClawDXWorkflowDual_ImplTimedOutAutoContinueRecoversToOk(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")
	fakeStepPath := filepath.Join(fakeBinDir, "fake-step.sh")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"demo","repo":"/tmp/demo"}],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":{},"error":null}'
    ;;
esac
`)

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
assistant=""
for ((i=1; i<=$#; i++)); do
  if [[ "${!i}" == "--assistant" ]]; then
    next=$((i+1))
    assistant="${!next}"
  fi
done
if [[ "$assistant" == "claude" ]]; then
  printf '%s' '{"ok":true,"mode":"run","status":"timed_out","overall_status":"timed_out","summary":"Implementation timed out.","agent_id":"agent-impl","workspace_id":"ws-1","assistant":"claude","next_action":"Continue implementation.","suggested_command":"skills/amux/scripts/openclaw-dx.sh continue --agent agent-impl --text \"Continue\" --enter","quick_actions":[],"channel":{"message":"impl timed out","chunks":["impl timed out"],"chunks_meta":[{"index":1,"total":1,"text":"impl timed out"}],"inline_buttons":[]}}'
  exit 0
fi
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"Review complete.","agent_id":"agent-review","workspace_id":"ws-1","assistant":"codex","next_action":"Ship changes.","suggested_command":"skills/amux/scripts/openclaw-dx.sh git ship --workspace ws-1","quick_actions":[],"channel":{"message":"review done","chunks":["review done"],"chunks_meta":[{"index":1,"total":1,"text":"review done"}],"inline_buttons":[]}}'
`)

	writeExecutable(t, fakeStepPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' '{"ok":true,"mode":"send","status":"idle","overall_status":"completed","summary":"Implementation resumed and completed.","agent_id":"agent-impl","workspace_id":"ws-1","assistant":"claude","next_action":"Run review.","suggested_command":"","quick_actions":[],"channel":{"message":"impl resumed","chunks":["impl resumed"],"chunks_meta":[{"index":1,"total":1,"text":"impl resumed"}],"inline_buttons":[]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "OPENCLAW_DX_STEP_SCRIPT", fakeStepPath)
	env = withEnv(env, "OPENCLAW_DX_SELF_SCRIPT", scriptPath)
	env = withEnv(env, "OPENCLAW_PRESENT_SCRIPT", "/nonexistent")

	payload := runScriptJSON(t, scriptPath, env,
		"workflow", "dual",
		"--workspace", "ws-1",
		"--implement-assistant", "claude",
		"--review-assistant", "codex",
		"--auto-continue-impl", "true",
	)

	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	implementation, ok := data["implementation"].(map[string]any)
	if !ok {
		t.Fatalf("implementation missing or wrong type: %T", data["implementation"])
	}
	if got, _ := implementation["status"].(string); got != "idle" {
		t.Fatalf("implementation.status = %q, want %q after auto-continue", got, "idle")
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "git ship --workspace ws-1") {
		t.Fatalf("suggested_command = %q, want ship command after recovery", suggested)
	}
}

func TestOpenClawDXWorkflowDual_ImplTimedOutFallsBackToConfiguredAssistant(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"demo","repo":"/tmp/demo"}],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":{},"error":null}'
    ;;
esac
`)

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
assistant=""
prompt=""
for ((i=1; i<=$#; i++)); do
  if [[ "${!i}" == "--assistant" ]]; then
    next=$((i+1))
    assistant="${!next}"
  fi
  if [[ "${!i}" == "--prompt" ]]; then
    next=$((i+1))
    prompt="${!next}"
  fi
done
if [[ "$prompt" == "Implement requested changes with tests." && "$assistant" == "claude" ]]; then
  printf '%s' '{"ok":true,"mode":"run","status":"timed_out","overall_status":"timed_out","summary":"Implementation timed out.","agent_id":"agent-impl-claude","workspace_id":"ws-1","assistant":"claude","next_action":"Continue implementation.","suggested_command":"","quick_actions":[],"channel":{"message":"impl timed out","chunks":["impl timed out"],"chunks_meta":[{"index":1,"total":1,"text":"impl timed out"}],"inline_buttons":[]}}'
  exit 0
fi
if [[ "$prompt" == "Implement requested changes with tests." && "$assistant" == "codex" ]]; then
  printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"Implementation completed by fallback.","agent_id":"agent-impl-codex","workspace_id":"ws-1","assistant":"codex","next_action":"Run review.","suggested_command":"","quick_actions":[],"channel":{"message":"impl fallback done","chunks":["impl fallback done"],"chunks_meta":[{"index":1,"total":1,"text":"impl fallback done"}],"inline_buttons":[]}}'
  exit 0
fi
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"Review completed.","agent_id":"agent-review","workspace_id":"ws-1","assistant":"codex","next_action":"Ship changes.","suggested_command":"skills/amux/scripts/openclaw-dx.sh git ship --workspace ws-1","quick_actions":[],"channel":{"message":"review done","chunks":["review done"],"chunks_meta":[{"index":1,"total":1,"text":"review done"}],"inline_buttons":[]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "OPENCLAW_DX_SELF_SCRIPT", scriptPath)
	env = withEnv(env, "OPENCLAW_PRESENT_SCRIPT", "/nonexistent")
	env = withEnv(env, "OPENCLAW_DX_IMPLEMENT_NEEDS_INPUT_FALLBACK_ASSISTANT", "codex")
	env = withEnv(env, "OPENCLAW_DX_KICKOFF_NEEDS_INPUT_AUTO_CONTINUE", "false")
	env = withEnv(env, "OPENCLAW_DX_TIMEOUT_RECOVERY", "false")

	payload := runScriptJSON(t, scriptPath, env,
		"workflow", "dual",
		"--workspace", "ws-1",
		"--implement-assistant", "claude",
		"--implement-prompt", "Implement requested changes with tests.",
		"--review-assistant", "codex",
		"--review-prompt", "Review current workspace changes for regressions.",
		"--auto-continue-impl", "false",
	)

	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["implement_assistant"].(string); got != "codex" {
		t.Fatalf("data.implement_assistant = %q, want %q", got, "codex")
	}
	implementation, ok := data["implementation"].(map[string]any)
	if !ok {
		t.Fatalf("implementation missing or wrong type: %T", data["implementation"])
	}
	if got, _ := implementation["status"].(string); got != "idle" {
		t.Fatalf("implementation status = %q, want %q", got, "idle")
	}
}
