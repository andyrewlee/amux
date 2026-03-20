package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistantTurnScript_AppliedChangesSummaryUsesLiveWorkspaceRootForCleanTreeGuard(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceID := "ws-live-root-clean"
	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-turn.sh")
	fakeDir := t.TempDir()
	fakeStepPath := filepath.Join(fakeDir, "fake-step.sh")
	fakeAmuxPath := filepath.Join(fakeDir, "amux")

	writeExecutable(t, fakeStepPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "${FAKE_STEP_JSON:?missing FAKE_STEP_JSON}"
`)
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "$*" in
  "workspace list --all")
    printf '%s' "${FAKE_WORKSPACE_LIST_JSON:?missing FAKE_WORKSPACE_LIST_JSON}"
    ;;
  "workspace list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "workspace list --archived --all")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "AMUX_ASSISTANT_TURN_STEP_SCRIPT", fakeStepPath)
	env = withEnv(env, "AMUX_BIN", fakeAmuxPath)
	env = withEnv(env, "FAKE_STEP_JSON", `{"ok":true,"mode":"run","status":"idle","summary":"Applied the changes and tests passed.","agent_id":"agent-live-root-clean","workspace_id":"`+workspaceID+`","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	env = withEnv(env, "FAKE_WORKSPACE_LIST_JSON", `{"ok":true,"data":[{"id":"`+workspaceID+`","root":"`+workspaceRoot+`"}],"error":null}`)

	payload := runScriptJSON(t, scriptPath, env,
		"run",
		"--workspace", workspaceID,
		"--assistant", "codex",
		"--prompt", "Handle the requested work",
		"--max-steps", "1",
		"--turn-budget", "30",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)

	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want live-workspace clean-tree warning", summary)
	}
}

func TestAssistantTurnScript_AppliedChangesSummaryFallsBackToArchivedRootWithoutAll(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceID := "ws-archived-root-clean"
	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-turn.sh")
	fakeDir := t.TempDir()
	fakeStepPath := filepath.Join(fakeDir, "fake-step.sh")
	fakeAmuxPath := filepath.Join(fakeDir, "amux")

	writeExecutable(t, fakeStepPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "${FAKE_STEP_JSON:?missing FAKE_STEP_JSON}"
`)
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "$*" in
  "workspace list --all")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "workspace list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "workspace list --archived --all")
    printf '%s\n' 'flag provided but not defined: --all' >&2
    exit 2
    ;;
  "workspace list --archived")
    printf '%s' "${FAKE_ARCHIVED_WORKSPACE_LIST_JSON:?missing FAKE_ARCHIVED_WORKSPACE_LIST_JSON}"
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "AMUX_ASSISTANT_TURN_STEP_SCRIPT", fakeStepPath)
	env = withEnv(env, "AMUX_BIN", fakeAmuxPath)
	env = withEnv(env, "FAKE_STEP_JSON", `{"ok":true,"mode":"run","status":"idle","summary":"Applied the changes and tests passed.","agent_id":"agent-archived-root-clean","workspace_id":"`+workspaceID+`","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	env = withEnv(env, "FAKE_ARCHIVED_WORKSPACE_LIST_JSON", `{"ok":true,"data":[{"id":"`+workspaceID+`","root":"`+workspaceRoot+`","archived":true}],"error":null}`)

	payload := runScriptJSON(t, scriptPath, env,
		"run",
		"--workspace", workspaceID,
		"--assistant", "codex",
		"--prompt", "Handle the requested work",
		"--max-steps", "1",
		"--turn-budget", "30",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)

	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want archived-workspace clean-tree warning via --archived fallback", summary)
	}
}
