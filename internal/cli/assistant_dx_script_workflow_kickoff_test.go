package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistantDXWorkflowKickoff_NeedsInputAddsContinueTurnQuickAction(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
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
printf '%s' '{"ok":true,"mode":"run","status":"needs_input","overall_status":"needs_input","summary":"Need target file.","agent_id":"agent-1","workspace_id":"ws-mobile","assistant":"codex","next_action":"Ask user which file to edit.","suggested_command":"skills/amux/scripts/assistant-dx.sh continue --agent agent-1 --text \"Continue safely.\" --enter","quick_actions":[{"id":"status","label":"Status","command":"skills/amux/scripts/assistant-dx.sh continue --agent agent-1 --text \"Provide a one-line progress status.\" --enter","style":"primary","prompt":"Get status"}],"channel":{"message":"needs input","chunks":["needs input"],"chunks_meta":[{"index":1,"total":1,"text":"needs input"}],"inline_buttons":[]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "AMUX_ASSISTANT_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "AMUX_ASSISTANT_DX_SELF_SCRIPT", scriptPath)
	env = withEnv(env, "AMUX_ASSISTANT_PRESENT_SCRIPT", "/nonexistent")

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

func TestAssistantDXWorkflowKickoff_AutoContinuesNeedsInputTurn(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
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
  printf '%s' '{"ok":true,"mode":"run","status":"needs_input","overall_status":"needs_input","summary":"Need target file.","agent_id":"agent-1","workspace_id":"ws-mobile","assistant":"codex","next_action":"Ask user which file to edit.","suggested_command":"skills/amux/scripts/assistant-dx.sh continue --agent agent-1 --text \"Continue safely.\" --enter","quick_actions":[],"channel":{"message":"needs input","chunks":["needs input"],"chunks_meta":[{"index":1,"total":1,"text":"needs input"}],"inline_buttons":[]}}'
  exit 0
fi
printf '%s' '{"ok":true,"mode":"send","status":"idle","overall_status":"completed","summary":"Auto-continued after safe default.","agent_id":"agent-1","workspace_id":"ws-mobile","assistant":"codex","next_action":"Run review.","suggested_command":"skills/amux/scripts/assistant-dx.sh review --workspace ws-mobile --assistant codex","quick_actions":[],"channel":{"message":"continued","chunks":["continued"],"chunks_meta":[{"index":1,"total":1,"text":"continued"}],"inline_buttons":[]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "AMUX_ASSISTANT_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "AMUX_ASSISTANT_DX_SELF_SCRIPT", scriptPath)
	env = withEnv(env, "AMUX_ASSISTANT_PRESENT_SCRIPT", "/nonexistent")

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

func TestAssistantDXWorkflowKickoff_ProjectNameUsesExistingRegisteredPath(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")
	projectAddLog := filepath.Join(fakeBinDir, "project-add.log")
	workspaceProjectLog := filepath.Join(fakeBinDir, "workspace-project.log")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project list")
    printf '%s' '{"ok":true,"data":[{"name":"checkfu","path":"/Users/andrewlee/founding/checkfu"}],"error":null}'
    ;;
  "project add")
    printf '%s' "${3:-}" > "${PROJECT_ADD_LOG:?missing PROJECT_ADD_LOG}"
    printf '%s' '{"ok":true,"data":{"name":"checkfu","path":"/Users/andrewlee/checkfu"},"error":null}'
    ;;
  "workspace create")
    project=""
    while [[ $# -gt 0 ]]; do
      if [[ "${1:-}" == "--project" && $# -ge 2 ]]; then
        project="$2"
        break
      fi
      shift
    done
    printf '%s' "$project" > "${WORKSPACE_PROJECT_LOG:?missing WORKSPACE_PROJECT_LOG}"
    printf '%s' '{"ok":true,"data":{"id":"ws-checkfu","name":"checkfu-review","repo":"/Users/andrewlee/founding/checkfu","root":"/tmp/ws-checkfu","assistant":"codex"},"error":null}'
    ;;
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-checkfu","name":"checkfu-review","repo":"/Users/andrewlee/founding/checkfu","root":"/tmp/ws-checkfu","assistant":"codex","created":"2026-02-22T00:00:00Z"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"Review complete.","agent_id":"agent-1","workspace_id":"ws-checkfu","assistant":"codex","next_action":"Open PR.","suggested_command":"","quick_actions":[],"channel":{"message":"done","chunks":["done"],"chunks_meta":[{"index":1,"total":1,"text":"done"}],"inline_buttons":[]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "PROJECT_ADD_LOG", projectAddLog)
	env = withEnv(env, "WORKSPACE_PROJECT_LOG", workspaceProjectLog)
	env = withEnv(env, "AMUX_ASSISTANT_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "AMUX_ASSISTANT_DX_SELF_SCRIPT", scriptPath)
	env = withEnv(env, "AMUX_ASSISTANT_PRESENT_SCRIPT", "/nonexistent")

	payload := runScriptJSON(t, scriptPath, env,
		"workflow", "kickoff",
		"--project", "checkfu",
		"--name", "checkfu-review",
		"--assistant", "codex",
		"--prompt", "Review uncommitted changes.",
	)

	if got, _ := payload["status"].(string); got == "command_error" {
		t.Fatalf("status = %q, want non-command_error", got)
	}

	projectArgRaw, err := os.ReadFile(workspaceProjectLog)
	if err != nil {
		t.Fatalf("read workspace project log: %v", err)
	}
	if got := strings.TrimSpace(string(projectArgRaw)); got != "/Users/andrewlee/founding/checkfu" {
		t.Fatalf("workspace create --project = %q, want %q", got, "/Users/andrewlee/founding/checkfu")
	}

	if _, err := os.Stat(projectAddLog); err == nil {
		projectAddRaw, readErr := os.ReadFile(projectAddLog)
		if readErr != nil {
			t.Fatalf("read project add log: %v", readErr)
		}
		if strings.TrimSpace(string(projectAddRaw)) != "" {
			t.Fatalf("unexpected project add call for bare-name selector: %q", strings.TrimSpace(string(projectAddRaw)))
		}
	}
}

func TestAssistantDXWorkflowKickoff_ProjectNameAmbiguousReturnsCommandError(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")
	projectAddLog := filepath.Join(fakeBinDir, "project-add.log")
	workspaceCreateLog := filepath.Join(fakeBinDir, "workspace-create.log")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project list")
    printf '%s' '{"ok":true,"data":[{"name":"checkfu","path":"/Users/andrewlee/founding/checkfu"},{"name":"checkfu","path":"/Users/andrewlee/client/checkfu"}],"error":null}'
    ;;
  "project add")
    printf '%s' "${3:-}" > "${PROJECT_ADD_LOG:?missing PROJECT_ADD_LOG}"
    printf '%s' '{"ok":true,"data":{"name":"checkfu","path":"/Users/andrewlee/checkfu"},"error":null}'
    ;;
  "workspace create")
    printf '%s' "called" > "${WORKSPACE_CREATE_LOG:?missing WORKSPACE_CREATE_LOG}"
    printf '%s' '{"ok":true,"data":{"id":"ws-checkfu","name":"checkfu-review","repo":"/Users/andrewlee/founding/checkfu","root":"/tmp/ws-checkfu","assistant":"codex"},"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
esac
`)

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"done","agent_id":"agent-1","workspace_id":"ws-checkfu","assistant":"codex","next_action":"","suggested_command":"","quick_actions":[],"channel":{"message":"done","chunks":["done"],"chunks_meta":[{"index":1,"total":1,"text":"done"}],"inline_buttons":[]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "PROJECT_ADD_LOG", projectAddLog)
	env = withEnv(env, "WORKSPACE_CREATE_LOG", workspaceCreateLog)
	env = withEnv(env, "AMUX_ASSISTANT_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "AMUX_ASSISTANT_DX_SELF_SCRIPT", scriptPath)
	env = withEnv(env, "AMUX_ASSISTANT_PRESENT_SCRIPT", "/nonexistent")

	payload := runScriptJSON(t, scriptPath, env,
		"workflow", "kickoff",
		"--project", "checkfu",
		"--name", "checkfu-review",
		"--assistant", "codex",
		"--prompt", "Review uncommitted changes.",
	)

	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "matches multiple registered projects") {
		t.Fatalf("summary = %q, want ambiguity guidance", summary)
	}

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want map[string]any", payload["data"])
	}
	errData, ok := data["error"].(map[string]any)
	if !ok {
		t.Fatalf("data.error = %T, want map[string]any", data["error"])
	}
	if got, _ := errData["code"].(string); got != "ambiguous_project_name" {
		t.Fatalf("data.error.code = %q, want %q", got, "ambiguous_project_name")
	}

	if _, err := os.Stat(projectAddLog); err == nil {
		projectAddRaw, readErr := os.ReadFile(projectAddLog)
		if readErr != nil {
			t.Fatalf("read project add log: %v", readErr)
		}
		if strings.TrimSpace(string(projectAddRaw)) != "" {
			t.Fatalf("unexpected project add call for ambiguous selector: %q", strings.TrimSpace(string(projectAddRaw)))
		}
	}
	if _, err := os.Stat(workspaceCreateLog); err == nil {
		t.Fatalf("workspace create should not be called for ambiguous bare-name selector")
	}
}

func TestAssistantDXWorkflowKickoff_DoesNotAutoContinueWhenPromptHasExplicitChoices(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")
	sendCalledPath := filepath.Join(fakeBinDir, "send-called.txt")

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
  printf '%s' '{"ok":true,"mode":"run","status":"needs_input","overall_status":"needs_input","summary":"Pick one:\n1. Continue with codex\n2. Continue with claude","input_hint":"Pick one:\n1. Continue with codex\n2. Continue with claude","agent_id":"agent-1","workspace_id":"ws-mobile","assistant":"codex","next_action":"","suggested_command":"","quick_actions":[],"channel":{"message":"needs input","chunks":["needs input"],"chunks_meta":[{"index":1,"total":1,"text":"needs input"}],"inline_buttons":[]}}'
  exit 0
fi
printf '%s' "called" > "${SEND_CALLED_PATH:?missing SEND_CALLED_PATH}"
printf '%s' '{"ok":true,"mode":"send","status":"idle","overall_status":"completed","summary":"should not auto-continue","agent_id":"agent-1","workspace_id":"ws-mobile","assistant":"codex","next_action":"Run review.","suggested_command":"","quick_actions":[],"channel":{"message":"continued","chunks":["continued"],"chunks_meta":[{"index":1,"total":1,"text":"continued"}],"inline_buttons":[]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "AMUX_ASSISTANT_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "AMUX_ASSISTANT_DX_SELF_SCRIPT", scriptPath)
	env = withEnv(env, "AMUX_ASSISTANT_PRESENT_SCRIPT", "/nonexistent")
	env = withEnv(env, "SEND_CALLED_PATH", sendCalledPath)

	payload := runScriptJSON(t, scriptPath, env,
		"workflow", "kickoff",
		"--project", "/tmp/demo",
		"--name", "mobile",
		"--assistant", "codex",
		"--prompt", "Fix the highest-impact debt item.",
	)

	if got, _ := payload["status"].(string); got != "needs_input" {
		t.Fatalf("status = %q, want %q when explicit choices require user decision", got, "needs_input")
	}
	nextAction, _ := payload["next_action"].(string)
	if !strings.Contains(nextAction, "choose one of the offered options") {
		t.Fatalf("next_action = %q, want explicit choice guidance", nextAction)
	}
	if _, err := os.Stat(sendCalledPath); err == nil {
		t.Fatalf("unexpected auto-continue send invocation for explicit choice prompt")
	}
}
