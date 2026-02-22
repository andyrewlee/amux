package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXStart_CommandErrorFallbackUsesWorkspaceStatus(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"mobile","repo":"/tmp/demo","root":"/tmp/ws-1","assistant":"codex","created":"2026-02-22T00:00:00Z"}],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
esac
`)

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' '{"ok":true,"mode":"run","status":"command_error","overall_status":"partial","summary":"Partial after 1 step(s). amux command failed","agent_id":"","workspace_id":"","assistant":"","next_action":"","suggested_command":"","quick_actions":[],"channel":{"message":"partial","chunks":["partial"],"chunks_meta":[{"index":1,"total":1,"text":"partial"}]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "OPENCLAW_DX_START_STEP_FALLBACK", "false")

	payload := runScriptJSON(t, scriptPath, env,
		"start",
		"--workspace", "ws-1",
		"--assistant", "codex",
		"--prompt", "Do work.",
	)

	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	nextAction, _ := payload["next_action"].(string)
	if !strings.Contains(nextAction, "assistant readiness") {
		t.Fatalf("next_action = %q, want assistant-readiness guidance", nextAction)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "status --workspace ws-1") {
		t.Fatalf("suggested_command = %q, want workspace status fallback", suggested)
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawWSStatus bool
	var sawAssistants bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		command, _ := action["command"].(string)
		if id == "status_ws" && strings.Contains(command, "status --workspace ws-1") {
			sawWSStatus = true
		}
		if id == "assistants_ws" && strings.Contains(command, "assistants --workspace ws-1 --probe --limit 3") {
			sawAssistants = true
		}
	}
	if !sawWSStatus {
		t.Fatalf("expected status_ws quick action in %#v", quickActions)
	}
	if !sawAssistants {
		t.Fatalf("expected assistants_ws quick action in %#v", quickActions)
	}
}

func TestOpenClawDXStart_FallsBackToStepRunOnTurnCommandFailure(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"mobile","repo":"/tmp/demo","root":"/tmp/ws-1","assistant":"codex"}],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
esac
`)

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' '{"ok":true,"mode":"run","status":"command_error","overall_status":"partial","summary":"Partial after 1 step(s). amux command failed","agent_id":"","workspace_id":"","assistant":"","next_action":"","suggested_command":"","quick_actions":[],"channel":{"message":"partial","chunks":["partial"],"chunks_meta":[{"index":1,"total":1,"text":"partial"}]}}'
`)

	writeExecutable(t, fakeStepPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"Recovered via step run","agent_id":"agent-1","workspace_id":"ws-1","assistant":"codex","next_action":"Continue.","suggested_command":"","quick_actions":[],"channel":{"message":"done","chunks":["done"],"chunks_meta":[{"index":1,"total":1,"text":"done"}]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "OPENCLAW_DX_STEP_SCRIPT", fakeStepPath)

	payload := runScriptJSON(t, scriptPath, env,
		"start",
		"--workspace", "ws-1",
		"--assistant", "codex",
		"--prompt", "Do work.",
	)

	if got, _ := payload["status"].(string); got != "idle" {
		t.Fatalf("status = %q, want %q", got, "idle")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Recovered via step run") {
		t.Fatalf("summary = %q, want step-fallback summary", summary)
	}
}

func TestOpenClawDXStart_RetriesWithFallbackAssistantOnCommandFailure(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"mobile","repo":"/tmp/demo","root":"/tmp/ws-1","assistant":"codex"}],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":[],"error":null}'
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
if [[ "$assistant" == "codex" ]]; then
  printf '%s' '{"ok":true,"mode":"run","status":"command_error","overall_status":"partial","summary":"Partial after 1 step(s). amux command failed","agent_id":"","workspace_id":"","assistant":"","next_action":"","suggested_command":"","quick_actions":[],"channel":{"message":"partial","chunks":["partial"],"chunks_meta":[{"index":1,"total":1,"text":"partial"}]}}'
  exit 0
fi
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"fallback assistant recovered","agent_id":"agent-1","workspace_id":"ws-1","assistant":"gemini","next_action":"Continue.","suggested_command":"","quick_actions":[],"channel":{"message":"done","chunks":["done"],"chunks_meta":[{"index":1,"total":1,"text":"done"}]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "OPENCLAW_DX_START_STEP_FALLBACK", "false")
	env = withEnv(env, "OPENCLAW_DX_START_COMMAND_ERROR_FALLBACK_ASSISTANT", "gemini")

	payload := runScriptJSON(t, scriptPath, env,
		"start",
		"--workspace", "ws-1",
		"--assistant", "codex",
		"--prompt", "Do work.",
	)

	if got, _ := payload["status"].(string); got != "idle" {
		t.Fatalf("status = %q, want %q", got, "idle")
	}
	if got, _ := payload["assistant"].(string); got != "gemini" {
		t.Fatalf("assistant = %q, want fallback assistant gemini", got)
	}
}

func TestOpenClawDXStart_PrunesStaleSessionsAndRetriesOnCommandFailure(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")
	pruneLog := filepath.Join(fakeBinDir, "prune.log")
	turnCountPath := filepath.Join(fakeBinDir, "turn-count.txt")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"mobile","repo":"/tmp/demo","root":"/tmp/ws-1","assistant":"codex"}],"error":null}'
    ;;
  "session prune")
    printf '%s' "pruned" > "${PRUNE_LOG:?missing PRUNE_LOG}"
    printf '%s' '{"ok":true,"data":{"dry_run":false,"pruned":[],"total":1,"errors":[]},"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
esac
`)

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
count=0
if [[ -f "${TURN_COUNT_PATH:?missing TURN_COUNT_PATH}" ]]; then
  count="$(cat "${TURN_COUNT_PATH}")"
fi
count=$((count + 1))
printf '%s' "$count" > "${TURN_COUNT_PATH}"
if [[ "$count" -eq 1 ]]; then
  printf '%s' '{"ok":true,"mode":"run","status":"command_error","overall_status":"partial","summary":"Partial after 1 step(s). amux command failed","agent_id":"","workspace_id":"","assistant":"","next_action":"","suggested_command":"","quick_actions":[],"channel":{"message":"partial","chunks":["partial"],"chunks_meta":[{"index":1,"total":1,"text":"partial"}]}}'
  exit 0
fi
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"Recovered after prune retry","agent_id":"agent-1","workspace_id":"ws-1","assistant":"codex","next_action":"Continue.","suggested_command":"","quick_actions":[],"channel":{"message":"done","chunks":["done"],"chunks_meta":[{"index":1,"total":1,"text":"done"}]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "OPENCLAW_DX_START_STEP_FALLBACK", "false")
	env = withEnv(env, "OPENCLAW_DX_START_COMMAND_ERROR_FALLBACK_ASSISTANT", "")
	env = withEnv(env, "OPENCLAW_DX_START_COMMAND_ERROR_RETRY", "false")
	env = withEnv(env, "PRUNE_LOG", pruneLog)
	env = withEnv(env, "TURN_COUNT_PATH", turnCountPath)

	payload := runScriptJSON(t, scriptPath, env,
		"start",
		"--workspace", "ws-1",
		"--assistant", "codex",
		"--prompt", "Do work.",
	)

	if got, _ := payload["status"].(string); got != "idle" {
		t.Fatalf("status = %q, want %q", got, "idle")
	}
	if _, err := os.Stat(pruneLog); err != nil {
		t.Fatalf("expected prune log to exist: %v", err)
	}
	countRaw, err := os.ReadFile(turnCountPath)
	if err != nil {
		t.Fatalf("read turn count: %v", err)
	}
	if strings.TrimSpace(string(countRaw)) != "2" {
		t.Fatalf("turn invocation count = %q, want 2", strings.TrimSpace(string(countRaw)))
	}
}

func TestOpenClawDXStart_RetriesTransientAmuxCommandFailureOnce(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")
	turnCountPath := filepath.Join(fakeBinDir, "turn-count.txt")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"mobile","repo":"/tmp/demo","root":"/tmp/ws-1","assistant":"codex"}],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
esac
`)

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
count=0
if [[ -f "${TURN_COUNT_PATH:?missing TURN_COUNT_PATH}" ]]; then
  count="$(cat "${TURN_COUNT_PATH}")"
fi
count=$((count + 1))
printf '%s' "$count" > "${TURN_COUNT_PATH}"
if [[ "$count" -eq 1 ]]; then
  printf '%s' '{"ok":true,"mode":"run","status":"command_error","overall_status":"partial","summary":"Partial after 1 step(s). amux command failed","agent_id":"","workspace_id":"","assistant":"","next_action":"","suggested_command":"","quick_actions":[],"channel":{"message":"partial","chunks":["partial"],"chunks_meta":[{"index":1,"total":1,"text":"partial"}]}}'
  exit 0
fi
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"retry succeeded","agent_id":"agent-1","workspace_id":"ws-1","assistant":"codex","next_action":"Continue work.","suggested_command":"","quick_actions":[],"channel":{"message":"done","chunks":["done"],"chunks_meta":[{"index":1,"total":1,"text":"done"}]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "TURN_COUNT_PATH", turnCountPath)

	payload := runScriptJSON(t, scriptPath, env,
		"start",
		"--workspace", "ws-1",
		"--assistant", "codex",
		"--prompt", "Do work.",
	)

	if got, _ := payload["status"].(string); got != "idle" {
		t.Fatalf("status = %q, want %q", got, "idle")
	}
	countRaw, err := os.ReadFile(turnCountPath)
	if err != nil {
		t.Fatalf("read turn count: %v", err)
	}
	if strings.TrimSpace(string(countRaw)) != "2" {
		t.Fatalf("turn invocation count = %q, want 2", strings.TrimSpace(string(countRaw)))
	}
}
