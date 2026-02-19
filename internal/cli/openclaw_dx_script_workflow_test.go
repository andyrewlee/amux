package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXWorkflowKickoff_RegistersProjectCreatesWorkspaceAndStartsTurn(t *testing.T) {
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
  "project list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "project add")
    printf '%s' '{"ok":true,"data":{"name":"demo","path":"/tmp/demo"},"error":null}'
    ;;
  "workspace create")
    printf '%s' '{"ok":true,"data":{"id":"ws-mobile","name":"mobile","repo":"/tmp/demo","root":"/tmp/ws-mobile","assistant":"codex"},"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"Implemented first debt fix.","agent_id":"agent-1","workspace_id":"ws-mobile","assistant":"codex","next_action":"Run review.","suggested_command":"skills/amux/scripts/openclaw-dx.sh review --workspace ws-mobile --assistant codex","quick_actions":[{"id":"continue","label":"Continue","command":"skills/amux/scripts/openclaw-dx.sh continue --workspace ws-mobile --text \"Continue\" --enter","style":"primary","prompt":"Continue current work"}],"channel":{"message":"done","chunks":["done"],"chunks_meta":[{"index":1,"total":1,"text":"done"}],"inline_buttons":[]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "OPENCLAW_DX_SELF_SCRIPT", scriptPath)

	payload := runScriptJSON(t, scriptPath, env,
		"workflow", "kickoff",
		"--project", "/tmp/demo",
		"--name", "mobile",
		"--assistant", "codex",
		"--prompt", "Fix the highest-impact debt item.",
	)

	if got, _ := payload["command"].(string); got != "workflow.kickoff" {
		t.Fatalf("command = %q, want %q", got, "workflow.kickoff")
	}
	if got, _ := payload["workflow"].(string); got != "kickoff" {
		t.Fatalf("workflow = %q, want %q", got, "kickoff")
	}
	kickoff, ok := payload["kickoff"].(map[string]any)
	if !ok {
		t.Fatalf("kickoff missing or wrong type: %T", payload["kickoff"])
	}
	if got, _ := kickoff["workspace_id"].(string); got != "ws-mobile" {
		t.Fatalf("workspace_id = %q, want %q", got, "ws-mobile")
	}

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawStatusWS bool
	var sawReviewWS bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		if id == "status_ws" {
			sawStatusWS = true
		}
		if id == "review_ws" {
			sawReviewWS = true
		}
	}
	if !sawStatusWS || !sawReviewWS {
		t.Fatalf("expected kickoff quick actions status_ws and review_ws, got %#v", quickActions)
	}
}

func TestOpenClawDXWorkflowDual_RunsImplementationThenReview(t *testing.T) {
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
printf '%s' '{"ok":true,"data":{},"error":null}'
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
  printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"Implemented refactor and tests.","agent_id":"agent-impl","workspace_id":"ws-1","assistant":"claude","next_action":"Run review.","suggested_command":"skills/amux/scripts/openclaw-dx.sh review --workspace ws-1 --assistant codex","quick_actions":[],"channel":{"message":"impl done","chunks":["impl done"],"chunks_meta":[{"index":1,"total":1,"text":"impl done"}],"inline_buttons":[]}}'
  exit 0
fi
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"Review complete with no blockers.","agent_id":"agent-review","workspace_id":"ws-1","assistant":"codex","next_action":"Ship changes.","suggested_command":"skills/amux/scripts/openclaw-dx.sh git ship --workspace ws-1","quick_actions":[],"channel":{"message":"review done","chunks":["review done"],"chunks_meta":[{"index":1,"total":1,"text":"review done"}],"inline_buttons":[]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "OPENCLAW_DX_SELF_SCRIPT", scriptPath)

	payload := runScriptJSON(t, scriptPath, env,
		"workflow", "dual",
		"--workspace", "ws-1",
		"--implement-assistant", "claude",
		"--review-assistant", "codex",
	)

	if got, _ := payload["command"].(string); got != "workflow.dual" {
		t.Fatalf("command = %q, want %q", got, "workflow.dual")
	}
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["workspace"].(string); got != "ws-1" {
		t.Fatalf("workspace = %q, want %q", got, "ws-1")
	}
	implementation, ok := data["implementation"].(map[string]any)
	if !ok {
		t.Fatalf("implementation missing or wrong type: %T", data["implementation"])
	}
	if got, _ := implementation["assistant"].(string); got != "claude" {
		t.Fatalf("implementation assistant = %q, want %q", got, "claude")
	}
	review, ok := data["review"].(map[string]any)
	if !ok {
		t.Fatalf("review missing or wrong type: %T", data["review"])
	}
	if got, _ := review["assistant"].(string); got != "codex" {
		t.Fatalf("review assistant = %q, want %q", got, "codex")
	}

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawShip bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		if id == "ship" {
			sawShip = true
			break
		}
	}
	if !sawShip {
		t.Fatalf("expected ship quick action, got %#v", quickActions)
	}
}

func TestOpenClawDXWorkflowDual_NeedsInputSkipsReviewAndSuggestsCodexFallback(t *testing.T) {
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
printf '%s' '{"ok":true,"data":{},"error":null}'
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
  printf '%s' '{"ok":true,"mode":"run","status":"needs_input","overall_status":"needs_input","summary":"Needs local permission selection.","agent_id":"agent-impl","workspace_id":"ws-1","assistant":"claude","next_action":"Switch to a non-interactive assistant (e.g. codex) for this step.","suggested_command":"","quick_actions":[],"channel":{"message":"needs input","chunks":["needs input"],"chunks_meta":[{"index":1,"total":1,"text":"needs input"}],"inline_buttons":[]}}'
  exit 0
fi
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"review should not run","agent_id":"agent-review","workspace_id":"ws-1","assistant":"codex","next_action":"Ship.","suggested_command":"skills/amux/scripts/openclaw-dx.sh git ship --workspace ws-1","quick_actions":[],"channel":{"message":"review","chunks":["review"],"chunks_meta":[{"index":1,"total":1,"text":"review"}],"inline_buttons":[]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "OPENCLAW_DX_SELF_SCRIPT", scriptPath)

	payload := runScriptJSON(t, scriptPath, env,
		"workflow", "dual",
		"--workspace", "ws-1",
		"--implement-assistant", "claude",
		"--review-assistant", "codex",
	)

	if got, _ := payload["status"].(string); got != "needs_input" {
		t.Fatalf("status = %q, want %q", got, "needs_input")
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "--assistant codex") {
		t.Fatalf("suggested_command = %q, want codex fallback", suggested)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["review_skipped_reason"].(string); got != "implementation_not_ready" {
		t.Fatalf("review_skipped_reason = %q, want %q", got, "implementation_not_ready")
	}

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawSwitch bool
	var sawRunReview bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		if id == "switch_codex" {
			sawSwitch = true
		}
		if id == "run_review" {
			sawRunReview = true
		}
	}
	if !sawSwitch {
		t.Fatalf("expected switch_codex quick action, got %#v", quickActions)
	}
	if sawRunReview {
		t.Fatalf("did not expect run_review quick action when implementation is blocked: %#v", quickActions)
	}
}
