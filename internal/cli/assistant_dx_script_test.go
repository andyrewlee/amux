package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistantDXProjectList_UsesAmuxAndChannelMetadata(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[{"name":"api","path":"/tmp/api"},{"name":"mobile","path":"/tmp/mobile"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "AMUX_ASSISTANT_CHANNEL", "discord")

	payload := runScriptJSON(t, scriptPath, env, "project", "list", "--query", "api")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	assistant, ok := payload["assistant_ux"].(map[string]any)
	if !ok {
		t.Fatalf("assistant_ux missing or wrong type: %T", payload["assistant_ux"])
	}
	if got, _ := assistant["selected_channel"].(string); got != "discord" {
		t.Fatalf("assistant_ux.selected_channel = %q, want discord", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "1 project") {
		t.Fatalf("summary = %q, want filtered project count", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "assistant-dx.sh") {
		t.Fatalf("suggested_command = %q, want wrapper command", suggested)
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	first, ok := quickActions[0].(map[string]any)
	if !ok {
		t.Fatalf("quick_actions[0] wrong type: %T", quickActions[0])
	}
	if got, _ := first["action_id"].(string); got == "" {
		t.Fatalf("quick_actions[0].action_id is empty")
	}
}

func TestAssistantDXTaskStart_NoImplicitRetry(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	callLog := filepath.Join(fakeBinDir, "calls.log")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "task start")
    printf '%s' '{"ok":true,"data":{"mode":"run","status":"timed_out","overall_status":"in_progress","summary":"Task still running.","next_action":"Wait and re-check.","suggested_command":"amux --json task status --workspace ws-1 --assistant droid","quick_actions":[]},"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "AMUX_CALL_LOG", callLog)

	payload := runScriptJSON(t, scriptPath, env,
		"task", "start",
		"--workspace", "ws-1",
		"--assistant", "droid",
		"--prompt", "Review current uncommitted changes",
	)
	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want attention", got)
	}
	suggested, _ := payload["suggested_command"].(string)
	if strings.Contains(suggested, "amux --json") || !strings.Contains(suggested, "assistant-dx.sh task status --workspace ws-1 --assistant droid") {
		t.Fatalf("suggested_command = %q, want wrapper task status command", suggested)
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
	}

	raw, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one amux call, got %d: %q", len(lines), string(raw))
	}
	if !strings.Contains(lines[0], "task start") {
		t.Fatalf("call log = %q, want task start", lines[0])
	}
}

func TestAssistantDXContinue_WorkspaceResolvesAgentAndSendsOnce(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	callLog := filepath.Join(fakeBinDir, "calls.log")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "task status")
    printf '%s' '{"ok":true,"data":{"mode":"status","status":"attention","overall_status":"in_progress","summary":"Task running.","agent_id":"ws-1:t_agent","workspace_id":"ws-1","assistant":"droid","quick_actions":[]},"error":null}'
    ;;
  "agent send")
    printf '%s' '{"ok":true,"data":{"agent_id":"ws-1:t_agent","status":"delivered","summary":"Continue step completed.","response":{"status":"idle","summary":"Continue completed.","next_action":"Run status."}},"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "AMUX_CALL_LOG", callLog)

	payload := runScriptJSON(t, scriptPath, env,
		"continue",
		"--workspace", "ws-1",
		"--assistant", "droid",
		"--text", "Continue and summarize status.",
		"--enter",
	)
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}

	raw, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	logText := string(raw)
	if strings.Count(logText, "task status") != 1 {
		t.Fatalf("expected exactly one task status call, got log:\n%s", logText)
	}
	if strings.Count(logText, "agent send") != 1 {
		t.Fatalf("expected exactly one agent send call, got log:\n%s", logText)
	}
}

func TestAssistantDXReview_AliasesTaskStartPrompt(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	callLog := filepath.Join(fakeBinDir, "calls.log")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "task start")
    printf '%s' '{"ok":true,"data":{"mode":"run","status":"idle","overall_status":"completed","summary":"Review done.","next_action":"Ship changes.","suggested_command":"skills/amux/scripts/assistant-dx.sh git ship --workspace ws-9","quick_actions":[]},"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "AMUX_CALL_LOG", callLog)

	payload := runScriptJSON(t, scriptPath, env,
		"review",
		"--workspace", "ws-9",
		"--assistant", "droid",
	)
	if got, _ := payload["command"].(string); got != "review" {
		t.Fatalf("command = %q, want review", got)
	}
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}

	raw, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	logText := strings.TrimSpace(string(raw))
	if strings.Count(logText, "task start") != 1 {
		t.Fatalf("expected exactly one task start call, got:\n%s", logText)
	}
	if !strings.Contains(logText, "--prompt Review current uncommitted changes") {
		t.Fatalf("expected default review prompt in task start call, got:\n%s", logText)
	}
}

func TestAssistantDXGuide_DoesNotInvokeAmux(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	callLog := filepath.Join(fakeBinDir, "calls.log")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
printf '%s' '{"ok":false,"error":{"code":"should_not_call","message":"guide should not call amux"}}'
exit 0
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "AMUX_CALL_LOG", callLog)

	payload := runScriptJSON(t, scriptPath, env,
		"guide",
		"--workspace", "ws-1",
		"--assistant", "droid",
		"--task", "review uncommitted changes",
	)
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "task start --workspace ws-1 --assistant droid") {
		t.Fatalf("suggested_command = %q, want task start recommendation", suggested)
	}

	if raw, err := os.ReadFile(callLog); err == nil && strings.TrimSpace(string(raw)) != "" {
		t.Fatalf("guide unexpectedly called amux:\n%s", string(raw))
	}
}

func TestAssistantDXWorkflow_ReturnsCommandError(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	payload := runScriptJSON(t, scriptPath, os.Environ(), "workflow", "dual", "--workspace", "ws-1")

	if got, _ := payload["ok"].(bool); got {
		t.Fatalf("ok = true, want false")
	}
	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want command_error", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "workflow commands were removed") {
		t.Fatalf("summary = %q, want workflow removal guidance", summary)
	}
}

func TestAssistantDXReview_MonitorsUntilTerminalStatus(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	callLog := filepath.Join(fakeBinDir, "calls.log")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "task start")
    printf '%s' '{"ok":true,"data":{"mode":"run","status":"attention","overall_status":"in_progress","summary":"Task is still running.","quick_actions":[]},"error":null}'
    ;;
  "task status")
    printf '%s' '{"ok":true,"data":{"mode":"status","status":"idle","overall_status":"completed","summary":"Review completed.","quick_actions":[]},"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "AMUX_CALL_LOG", callLog)

	payload := runScriptJSON(t, scriptPath, env,
		"review",
		"--workspace", "ws-9",
		"--assistant", "droid",
		"--monitor-timeout", "30s",
		"--poll-interval", "1s",
	)
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	if got, _ := payload["summary"].(string); !strings.Contains(got, "Review completed") {
		t.Fatalf("summary = %q, want monitored completion summary", got)
	}

	raw, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	logText := strings.TrimSpace(string(raw))
	if strings.Count(logText, "task start") != 1 {
		t.Fatalf("expected exactly one task start call, got:\n%s", logText)
	}
	if strings.Count(logText, "task status") != 1 {
		t.Fatalf("expected exactly one task status poll, got:\n%s", logText)
	}
}

func TestAssistantDXWorkspaceCreate_UsesPositionalName(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	callLog := filepath.Join(fakeBinDir, "calls.log")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace create")
    printf '%s' '{"ok":true,"data":{"id":"ws-new","assistant":"droid"},"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "AMUX_CALL_LOG", callLog)

	payload := runScriptJSON(t, scriptPath, env,
		"workspace", "create", "feat-login",
		"--project", "/tmp/repo",
		"--assistant", "droid",
		"--base", "main",
	)
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	raw, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	logText := string(raw)
	if !strings.Contains(logText, "workspace create feat-login --project /tmp/repo --assistant droid --base main") {
		t.Fatalf("workspace create call mismatch:\n%s", logText)
	}
}

func TestAssistantDXWorkspaceList_RejectsUnsupportedFlags(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	payload := runScriptJSON(t, scriptPath, os.Environ(), "workspace", "list", "--limit", "10")
	if got, _ := payload["ok"].(bool); got {
		t.Fatalf("ok = true, want false")
	}
	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want command_error", got)
	}
}

func TestAssistantDXStatus_DoesNotDependOnSessionStatusField(t *testing.T) {
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
	  "workspace list")
	    printf '%s' '{"ok":true,"data":[{"id":"ws-a"},{"id":"ws-b"}],"error":null}'
	    ;;
	  "session list")
	    printf '%s' '{"ok":true,"data":[{"session_name":"s1","workspace_id":"ws-b","type":"agent","attached":false},{"session_name":"s2","workspace_id":"ws-a","type":"terminal","attached":true}],"error":null}'
	    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	payload := runScriptJSON(t, scriptPath, env, "status")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "agent session(s)") {
		t.Fatalf("summary = %q, want agent session summary", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "--workspace ws-b") {
		t.Fatalf("suggested_command = %q, want active agent workspace ws-b", suggested)
	}
}
