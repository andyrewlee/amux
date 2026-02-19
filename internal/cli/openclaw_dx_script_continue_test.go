package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXStatus_SurfacesNeedsInputAndStaleSessions(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project list")
    printf '%s' '{"ok":true,"data":[{"name":"demo","path":"/tmp/demo"}],"error":null}'
    ;;
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"demo","repo":"/tmp/demo","created":"2026-01-01T00:00:00Z"}],"error":null}'
    ;;
  "agent list")
    printf '%s' '{"ok":true,"data":[{"session_name":"sess-1","agent_id":"agent-1","workspace_id":"ws-1","tab_id":"tab-1","type":"agent"}],"error":null}'
    ;;
  "terminal list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[{"session_name":"sess-1","workspace_id":"ws-1","type":"agent","attached":false,"age_seconds":100}],"error":null}'
    ;;
  "session prune")
    printf '%s' '{"ok":true,"data":{"dry_run":true,"pruned":[],"total":2,"errors":[]},"error":null}'
    ;;
  "agent capture")
    printf '%s' '{"ok":true,"data":{"session_name":"sess-1","status":"captured","summary":"Need approval","needs_input":true,"input_hint":"Confirm strategy"},"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "status", "--limit", "3", "--include-stale")

	if got, _ := payload["command"].(string); got != "status" {
		t.Fatalf("command = %q, want %q", got, "status")
	}
	if got, _ := payload["status"].(string); got != "needs_input" {
		t.Fatalf("status = %q, want %q", got, "needs_input")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	alerts, ok := data["alerts"].([]any)
	if !ok || len(alerts) < 2 {
		t.Fatalf("alerts = %#v, want >=2", data["alerts"])
	}
	var sawNeedsInput bool
	var sawStale bool
	for _, raw := range alerts {
		alert, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch alert["type"] {
		case "needs_input":
			sawNeedsInput = true
		case "stale_sessions":
			sawStale = true
		}
	}
	if !sawNeedsInput {
		t.Fatalf("expected needs_input alert in %#v", alerts)
	}
	if !sawStale {
		t.Fatalf("expected stale_sessions alert in %#v", alerts)
	}
}

func TestOpenClawDXStatus_DefaultOmitsStaleSessionAlerts(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project list")
    printf '%s' '{"ok":true,"data":[{"name":"demo","path":"/tmp/demo"}],"error":null}'
    ;;
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"demo","repo":"/tmp/demo","created":"2026-01-01T00:00:00Z"}],"error":null}'
    ;;
  "agent list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "terminal list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "session prune")
    printf '%s' '{"ok":true,"data":{"dry_run":true,"pruned":[],"total":4,"errors":[]},"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "status")

	if got, _ := payload["command"].(string); got != "status" {
		t.Fatalf("command = %q, want %q", got, "status")
	}
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	alerts, ok := data["alerts"].([]any)
	if !ok {
		t.Fatalf("alerts missing or wrong type: %T", data["alerts"])
	}
	if len(alerts) != 0 {
		t.Fatalf("expected no alerts by default, got %#v", alerts)
	}
}

func TestOpenClawDXContinue_ResolvesWorkspaceAgentAndCallsTurnScript(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")
	argsLog := filepath.Join(fakeBinDir, "turn-args.log")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "agent list")
    printf '%s' '{"ok":true,"data":[{"session_name":"sess-1","agent_id":"agent-1","workspace_id":"ws-1","tab_id":"tab-1","type":"agent"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" > "${TURN_ARGS_LOG:?missing TURN_ARGS_LOG}"
printf '%s' '{"ok":true,"mode":"send","status":"idle","overall_status":"completed","summary":"continued","agent_id":"agent-1","workspace_id":"ws-1","channel":{"message":"done","chunks":["done"],"chunks_meta":[{"index":1,"total":1,"text":"done"}]},"quick_actions":[]}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "TURN_ARGS_LOG", argsLog)

	payload := runScriptJSON(t, scriptPath, env,
		"continue",
		"--workspace", "ws-1",
		"--text", "ping",
		"--enter",
	)

	if got, _ := payload["command"].(string); got != "continue" {
		t.Fatalf("command = %q, want %q", got, "continue")
	}
	if got, _ := payload["workflow"].(string); got != "followup_turn" {
		t.Fatalf("workflow = %q, want %q", got, "followup_turn")
	}
	argsRaw, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("read turn args: %v", err)
	}
	args := string(argsRaw)
	if !strings.Contains(args, "send") || !strings.Contains(args, "--agent agent-1") || !strings.Contains(args, "--text ping") || !strings.Contains(args, "--enter") {
		t.Fatalf("turn args = %q, expected send/agent/text/enter", args)
	}
}

func TestOpenClawDXContinue_PassthroughSanitizesNeedsInputAndAddsFallbackCommand(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' '{"ok":true,"data":{},"error":null}'
`)

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' '{"ok":true,"mode":"send","status":"needs_input","overall_status":"needs_input","summary":"Need guidance █","agent_id":"agent-1","next_action":"","suggested_command":"","quick_actions":[],"channel":{"message":"Need guidance █","chunks":["Need guidance █"],"chunks_meta":[{"index":1,"total":1,"text":"Need guidance █"}]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)

	payload := runScriptJSON(t, scriptPath, env,
		"continue",
		"--agent", "agent-1",
		"--text", "continue",
		"--enter",
	)

	if got, _ := payload["status"].(string); got != "needs_input" {
		t.Fatalf("status = %q, want %q", got, "needs_input")
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "█") {
		t.Fatalf("summary still contains cursor artifact: %q", summary)
	}
	nextAction, _ := payload["next_action"].(string)
	if strings.TrimSpace(nextAction) == "" {
		t.Fatalf("next_action should be auto-filled for needs_input")
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "openclaw-step.sh send --agent agent-1") {
		t.Fatalf("suggested_command = %q, want fallback step command", suggested)
	}
}

func TestOpenClawDXContinue_AutoStartsWhenNoActiveAgent(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")
	argsLog := filepath.Join(fakeBinDir, "turn-args.log")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "agent list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"mobile","assistant":"codex","repo":"/tmp/demo"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" > "${TURN_ARGS_LOG:?missing TURN_ARGS_LOG}"
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"auto-started","agent_id":"agent-1","workspace_id":"ws-1","channel":{"message":"done","chunks":["done"],"chunks_meta":[{"index":1,"total":1,"text":"done"}]},"quick_actions":[]}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "OPENCLAW_DX_SELF_SCRIPT", scriptPath)
	env = withEnv(env, "TURN_ARGS_LOG", argsLog)

	payload := runScriptJSON(t, scriptPath, env,
		"continue",
		"--workspace", "ws-1",
		"--auto-start",
		"--assistant", "codex",
		"--text", "Resume and report status.",
	)

	if got, _ := payload["command"].(string); got != "continue" {
		t.Fatalf("command = %q, want %q", got, "continue")
	}
	if got, _ := payload["workflow"].(string); got != "auto_start_turn" {
		t.Fatalf("workflow = %q, want %q", got, "auto_start_turn")
	}
	if got, _ := payload["auto_started"].(bool); !got {
		t.Fatalf("auto_started = %v, want true", got)
	}

	argsRaw, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("read turn args: %v", err)
	}
	args := strings.TrimSpace(string(argsRaw))
	if !strings.Contains(args, "run") ||
		!strings.Contains(args, "--workspace ws-1") ||
		!strings.Contains(args, "--assistant codex") ||
		!strings.Contains(args, "--prompt Resume and report status.") {
		t.Fatalf("turn args = %q, expected run/workspace/assistant/prompt", args)
	}
}

func TestOpenClawDXContinue_NoTargetUsesSingleActiveAgent(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")
	argsLog := filepath.Join(fakeBinDir, "turn-args.log")
	contextPath := filepath.Join(t.TempDir(), "context.json")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "agent list")
    printf '%s' '{"ok":true,"data":[{"session_name":"sess-1","agent_id":"agent-1","workspace_id":"ws-1","tab_id":"tab-1","type":"agent"}],"error":null}'
    ;;
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"mobile","repo":"/tmp/demo","assistant":"codex"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" > "${TURN_ARGS_LOG:?missing TURN_ARGS_LOG}"
printf '%s' '{"ok":true,"mode":"send","status":"idle","overall_status":"completed","summary":"continued","agent_id":"agent-1","workspace_id":"ws-1","channel":{"message":"done","chunks":["done"],"chunks_meta":[{"index":1,"total":1,"text":"done"}]},"quick_actions":[]}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "TURN_ARGS_LOG", argsLog)
	env = withEnv(env, "OPENCLAW_DX_CONTEXT_FILE", contextPath)

	payload := runScriptJSON(t, scriptPath, env,
		"continue",
		"--text", "Resume now.",
		"--enter",
	)

	if got, _ := payload["command"].(string); got != "continue" {
		t.Fatalf("command = %q, want %q", got, "continue")
	}
	if got, _ := payload["workflow"].(string); got != "followup_turn" {
		t.Fatalf("workflow = %q, want %q", got, "followup_turn")
	}

	argsRaw, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("read turn args: %v", err)
	}
	args := string(argsRaw)
	if !strings.Contains(args, "send") || !strings.Contains(args, "--agent agent-1") {
		t.Fatalf("turn args = %q, expected send with resolved single agent", args)
	}
}

func TestOpenClawDXContinue_NoTargetWithMultipleAgentsPromptsSelection(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	contextPath := filepath.Join(t.TempDir(), "context.json")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "agent list")
    printf '%s' '{"ok":true,"data":[{"session_name":"sess-a","agent_id":"agent-a","workspace_id":"ws-a"},{"session_name":"sess-b","agent_id":"agent-b","workspace_id":"ws-b"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_CONTEXT_FILE", contextPath)

	payload := runScriptJSON(t, scriptPath, env,
		"continue",
		"--text", "Resume now.",
		"--enter",
	)

	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "--agent agent-a") {
		t.Fatalf("suggested_command = %q, want agent selection command", suggested)
	}

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["reason"].(string); got != "multiple_active_agents" {
		t.Fatalf("reason = %q, want %q", got, "multiple_active_agents")
	}

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawFirst bool
	var sawSecond bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		cmd, _ := action["command"].(string)
		if id == "continue_1" && strings.Contains(cmd, "--agent agent-a") {
			sawFirst = true
		}
		if id == "continue_2" && strings.Contains(cmd, "--agent agent-b") {
			sawSecond = true
		}
	}
	if !sawFirst || !sawSecond {
		t.Fatalf("expected continue actions for both agents: %#v", quickActions)
	}
}
