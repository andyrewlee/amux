package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXWorkflowKickoff_NeedsInputAddsContinueTurnQuickAction(t *testing.T) {
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
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-mobile","name":"mobile","repo":"/tmp/demo","root":"/tmp/ws-mobile","assistant":"codex","created":"2026-02-22T00:00:00Z"}],"error":null}'
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
printf '%s' '{"ok":true,"mode":"run","status":"needs_input","overall_status":"needs_input","summary":"Need target file.","agent_id":"agent-1","workspace_id":"ws-mobile","assistant":"codex","next_action":"Ask user which file to edit.","suggested_command":"skills/amux/scripts/openclaw-dx.sh continue --agent agent-1 --text \"Continue safely.\" --enter","quick_actions":[{"id":"status","label":"Status","command":"skills/amux/scripts/openclaw-dx.sh continue --agent agent-1 --text \"Provide a one-line progress status.\" --enter","style":"primary","prompt":"Get status"}],"channel":{"message":"needs input","chunks":["needs input"],"chunks_meta":[{"index":1,"total":1,"text":"needs input"}],"inline_buttons":[]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "OPENCLAW_DX_SELF_SCRIPT", scriptPath)
	env = withEnv(env, "OPENCLAW_PRESENT_SCRIPT", "/nonexistent")

	payload := runScriptJSON(t, scriptPath, env,
		"workflow", "kickoff",
		"--project", "/tmp/demo",
		"--name", "mobile",
		"--assistant", "codex",
		"--prompt", "Fix the highest-impact debt item.",
	)

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	suggested, _ := payload["suggested_command"].(string)

	var sawContinueTurn bool
	var sawReplyContinue bool
	var sawWSStatus bool
	var sawWSReview bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		label, _ := action["label"].(string)
		switch id {
		case "continue_turn":
			command, _ := action["command"].(string)
			sawContinueTurn = strings.Contains(command, "continue --agent agent-1")
		case "status":
			sawReplyContinue = label == "Reply + Continue"
		case "status_ws":
			sawWSStatus = label == "WS Status"
		case "review_ws":
			sawWSReview = label == "WS Review"
		}
	}
	if suggested != "" && !sawContinueTurn && !sawReplyContinue {
		t.Fatalf("expected continue action hint (continue_turn or Reply + Continue) in %#v", quickActions)
	}
	if !sawWSStatus || !sawWSReview {
		t.Fatalf("expected WS status/review quick action labels in %#v", quickActions)
	}
}

func TestOpenClawDXWorkflowKickoff_AutoContinuesNeedsInputTurn(t *testing.T) {
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
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-mobile","name":"mobile","repo":"/tmp/demo","root":"/tmp/ws-mobile","assistant":"codex","created":"2026-02-22T00:00:00Z"}],"error":null}'
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
mode="${1:-}"
if [[ "$mode" == "run" ]]; then
  printf '%s' '{"ok":true,"mode":"run","status":"needs_input","overall_status":"needs_input","summary":"Need target file.","agent_id":"agent-1","workspace_id":"ws-mobile","assistant":"codex","next_action":"Ask user which file to edit.","suggested_command":"skills/amux/scripts/openclaw-dx.sh continue --agent agent-1 --text \"Continue safely.\" --enter","quick_actions":[],"channel":{"message":"needs input","chunks":["needs input"],"chunks_meta":[{"index":1,"total":1,"text":"needs input"}],"inline_buttons":[]}}'
  exit 0
fi
printf '%s' '{"ok":true,"mode":"send","status":"idle","overall_status":"completed","summary":"Auto-continued after safe default.","agent_id":"agent-1","workspace_id":"ws-mobile","assistant":"codex","next_action":"Run review.","suggested_command":"skills/amux/scripts/openclaw-dx.sh review --workspace ws-mobile --assistant codex","quick_actions":[],"channel":{"message":"continued","chunks":["continued"],"chunks_meta":[{"index":1,"total":1,"text":"continued"}],"inline_buttons":[]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "OPENCLAW_DX_SELF_SCRIPT", scriptPath)
	env = withEnv(env, "OPENCLAW_PRESENT_SCRIPT", "/nonexistent")

	payload := runScriptJSON(t, scriptPath, env,
		"workflow", "kickoff",
		"--project", "/tmp/demo",
		"--name", "mobile",
		"--assistant", "codex",
		"--prompt", "Fix the highest-impact debt item.",
	)

	if got, _ := payload["status"].(string); got == "needs_input" {
		t.Fatalf("status = %q, expected auto-continued non-needs_input status", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Auto-continued") {
		t.Fatalf("summary = %q, want auto-continue summary", summary)
	}
}
