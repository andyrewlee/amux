package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistantDXProjectList_EmitsJSONOnAmuxError(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project list")
    printf '%s' '{"ok":false,"error":{"code":"boom","message":"fail"}}'
    ;;
  *)
    printf '%s' '{"ok":false,"error":{"code":"unexpected","message":"unexpected args"}}'
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "project", "list")
	if got, _ := payload["ok"].(bool); got {
		t.Fatalf("ok = true, want false")
	}
	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want command_error", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "fail") {
		t.Fatalf("summary = %q, want error message", summary)
	}
}

func TestAssistantDXProjectList_UsesJSONErrorEnvelopeWhenAmuxExitsNonZero(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project list")
    printf '%s' '{"ok":false,"error":{"code":"boom","message":"fail"}}'
    exit 3
    ;;
  *)
    printf '%s' '{"ok":false,"error":{"code":"unexpected","message":"unexpected args"}}'
    exit 2
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "project", "list")
	if got, _ := payload["ok"].(bool); got {
		t.Fatalf("ok = true, want false")
	}
	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want command_error", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "fail") {
		t.Fatalf("summary = %q, want amux JSON error message", summary)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["details"].(string); got != "boom" {
		t.Fatalf("data.details = %q, want boom", got)
	}
}

func TestAssistantDXStatusWorkspace_NormalizesFollowupsToWrapperCommands(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "task status")
    printf '%s' '{"ok":true,"data":{"status":"needs_input","overall_status":"needs_input","summary":"Need input.","next_action":"Reply with one option.","suggested_command":"amux --json agent send --agent ws-1:t_agent --text hi --wait","quick_actions":[{"id":"send","label":"Send","command":"amux --json agent send --agent ws-1:t_agent --text hi --wait","style":"primary"}]},"error":null}'
    ;;
  *)
    printf '%s' '{"ok":false,"error":{"code":"unexpected","message":"unexpected args"}}'
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "status", "--workspace", "ws-1", "--assistant", "droid")
	if got, _ := payload["status"].(string); got != "needs_input" {
		t.Fatalf("status = %q, want needs_input", got)
	}
	suggested, _ := payload["suggested_command"].(string)
	if strings.Contains(suggested, "amux --json") || !strings.Contains(suggested, "assistant-dx.sh continue --workspace ws-1 --assistant droid") {
		t.Fatalf("suggested_command = %q, want wrapper continue command", suggested)
	}
	if strings.Contains(suggested, "<your response>") {
		t.Fatalf("suggested_command = %q, must not include literal placeholder text", suggested)
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	for i, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("quick_actions[%d] wrong type: %T", i, raw)
		}
		cmd, _ := action["command"].(string)
		if strings.Contains(cmd, "amux --json") || !strings.Contains(cmd, "assistant-dx.sh") {
			t.Fatalf("quick_actions[%d].command = %q, want wrapper command", i, cmd)
		}
		if strings.Contains(cmd, "<your response>") {
			t.Fatalf("quick_actions[%d].command = %q, must not include literal placeholder text", i, cmd)
		}
	}
}

func TestAssistantDXProjectList_NonJSONNonZeroClassifiedAsCommandFailed(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project list")
    printf '%s\n' 'transport failure' >&2
    exit 2
    ;;
  *)
    printf '%s\n' 'unexpected args' >&2
    exit 2
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "project", "list")
	if got, _ := payload["ok"].(bool); got {
		t.Fatalf("ok = true, want false")
	}
	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want command_error", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "amux command failed") {
		t.Fatalf("summary = %q, want command failed classification", summary)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	details, _ := data["details"].(string)
	if !strings.Contains(details, "transport failure") {
		t.Fatalf("data.details = %q, want stderr content", details)
	}
}

func TestAssistantDXProjectList_InvalidJSONEnvelopeClassifiedAsInvalidJSON(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project list")
    printf '%s' '[]'
    ;;
  *)
    printf '%s' '{"ok":false,"error":{"code":"unexpected","message":"unexpected args"}}'
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "project", "list")
	if got, _ := payload["ok"].(bool); got {
		t.Fatalf("ok = true, want false")
	}
	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want command_error", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "amux returned invalid JSON") {
		t.Fatalf("summary = %q, want invalid JSON classification", summary)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["details"].(string); got != "[]" {
		t.Fatalf("data.details = %q, want []", got)
	}
}

func TestAssistantDXStatusWorkspace_CompletedSuggestsContinue(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "task status")
    printf '%s' '{"ok":true,"data":{"status":"idle","overall_status":"completed","agent_id":"ws-2:t_agent","summary":"Task completed.","next_action":"Share results and continue if needed.","quick_actions":[{"id":"status","label":"Status","command":"amux --json task status --workspace ws-2 --assistant droid","style":"primary"}]},"error":null}'
    ;;
  *)
    printf '%s' '{"ok":false,"error":{"code":"unexpected","message":"unexpected args"}}'
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "status", "--workspace", "ws-2", "--assistant", "droid")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "assistant-dx.sh continue --workspace ws-2 --assistant droid") {
		t.Fatalf("suggested_command = %q, want wrapper continue command", suggested)
	}
	if strings.Contains(suggested, "task status --workspace ws-2") {
		t.Fatalf("suggested_command = %q, should not dead-end to self status", suggested)
	}

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	foundContinue := false
	for i, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("quick_actions[%d] wrong type: %T", i, raw)
		}
		id, _ := action["action_id"].(string)
		cmd, _ := action["command"].(string)
		if strings.Contains(cmd, "amux --json") {
			t.Fatalf("quick_actions[%d].command = %q, expected wrapper command", i, cmd)
		}
		if id == "continue" {
			foundContinue = true
		}
	}
	if !foundContinue {
		t.Fatalf("quick_actions missing continue action: %#v", quickActions)
	}
}

func TestAssistantDXStatusWorkspace_CompletedWithoutAgentSuggestsStart(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "task status")
    printf '%s' '{"ok":true,"data":{"status":"idle","overall_status":"completed","summary":"No active task run found.","next_action":"Start a task when ready.","quick_actions":[{"id":"status","label":"Status","command":"amux --json task status --workspace ws-3 --assistant droid","style":"primary"}]},"error":null}'
    ;;
  *)
    printf '%s' '{"ok":false,"error":{"code":"unexpected","message":"unexpected args"}}'
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "status", "--workspace", "ws-3", "--assistant", "droid")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "assistant-dx.sh task start --workspace ws-3 --assistant droid") {
		t.Fatalf("suggested_command = %q, want wrapper task start command", suggested)
	}
	if strings.Contains(suggested, "continue --workspace ws-3") {
		t.Fatalf("suggested_command = %q, should not suggest continue with no active agent", suggested)
	}

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	foundStart := false
	for i, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("quick_actions[%d] wrong type: %T", i, raw)
		}
		id, _ := action["action_id"].(string)
		cmd, _ := action["command"].(string)
		if strings.Contains(cmd, "amux --json") {
			t.Fatalf("quick_actions[%d].command = %q, expected wrapper command", i, cmd)
		}
		if id == "start" {
			foundStart = true
		}
		if id == "continue" {
			t.Fatalf("quick_actions should not include continue when no active agent: %#v", quickActions)
		}
	}
	if !foundStart {
		t.Fatalf("quick_actions missing start action: %#v", quickActions)
	}
}
