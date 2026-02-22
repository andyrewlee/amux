package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXCommandError_DefaultsToGuideSuggestion(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	contextPath := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(contextPath, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' '{"ok":true,"data":[],"error":null}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_CONTEXT_FILE", contextPath)

	payload := runScriptJSON(t, scriptPath, env, "status", "--bad-flag")

	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "openclaw-dx.sh guide") {
		t.Fatalf("suggested_command = %q, want guide fallback", suggested)
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
}

func TestOpenClawDXCommandError_UsesWorkspaceStatusWhenContextExists(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	contextPath := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(contextPath, []byte(`{"workspace":{"id":"ws-1"}}`), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' '{"ok":true,"data":[],"error":null}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_CONTEXT_FILE", contextPath)

	payload := runScriptJSON(t, scriptPath, env, "status", "--bad-flag")

	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "openclaw-dx.sh guide") {
		t.Fatalf("suggested_command = %q, want guide fallback", suggested)
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawWSStatus bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		command, _ := action["command"].(string)
		if id == "status_ws" && strings.Contains(command, "status --workspace ws-1") {
			sawWSStatus = true
			break
		}
	}
	if !sawWSStatus {
		t.Fatalf("expected workspace status quick action in %#v", quickActions)
	}
}

func TestOpenClawDXAmuxError_TerminalCommandSuggestsWorkspaceLogs(t *testing.T) {
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
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"mobile","repo":"/tmp/demo","root":"/tmp/ws-1","assistant":"codex"}],"error":null}'
    ;;
  "terminal run")
    printf '%s' '{"ok":false,"error":{"code":"terminal_failed","message":"server exited unexpectedly","details":{}}}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"terminal", "run",
		"--workspace", "ws-1",
		"--text", "pwd",
		"--enter",
	)

	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	nextAction, _ := payload["next_action"].(string)
	if !strings.Contains(nextAction, "backend exited unexpectedly") {
		t.Fatalf("next_action = %q, want backend-exit guidance", nextAction)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "terminal logs --workspace ws-1 --lines 120") {
		t.Fatalf("suggested_command = %q, want workspace terminal logs command", suggested)
	}
}

func TestOpenClawDXAmuxError_TerminalSessionCreateSuggestsInitCommand(t *testing.T) {
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
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"mobile","repo":"/tmp/demo","root":"/tmp/ws-1","assistant":"codex"}],"error":null}'
    ;;
  "terminal run")
    printf '%s' '{"ok":false,"error":{"code":"session_create_failed","message":"exit status 1","details":{"workspace_id":"ws-1"}}}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"terminal", "preset",
		"--workspace", "ws-1",
		"--kind", "nextjs",
	)

	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Terminal session start failed for workspace") {
		t.Fatalf("summary = %q, want workspace-specific terminal start failure", summary)
	}
	nextAction, _ := payload["next_action"].(string)
	if !strings.Contains(nextAction, "could not be created") {
		t.Fatalf("next_action = %q, want session-create guidance", nextAction)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "terminal run --workspace ws-1") || !strings.Contains(suggested, "--text \"pwd\" --enter") {
		t.Fatalf("suggested_command = %q, want terminal init command", suggested)
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawInit bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		command, _ := action["command"].(string)
		if id == "terminal_init_ws" && strings.Contains(command, "terminal run --workspace ws-1") {
			sawInit = true
			break
		}
	}
	if !sawInit {
		t.Fatalf("expected terminal_init_ws quick action in %#v", quickActions)
	}
}

func TestOpenClawDXParseError_MissingFlagValueReturnsCommandErrorJSON(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' '{"ok":true,"data":[],"error":null}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "start", "--workspace")

	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "missing value for --workspace") {
		t.Fatalf("summary = %q, want missing value guidance", summary)
	}
}

func TestOpenClawDXStart_AllowsDashPrefixedPromptValue(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "openclaw-turn.sh")
	turnArgsLog := filepath.Join(t.TempDir(), "turn-args.log")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"demo","repo":"/tmp/demo","assistant":"codex"}],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
esac
`)
	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" > "${TURN_ARGS_LOG:?missing TURN_ARGS_LOG}"
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"started","agent_id":"agent-1","workspace_id":"ws-1","assistant":"codex","quick_actions":[],"channel":{"message":"done","chunks":["done"],"chunks_meta":[{"index":1,"total":1,"text":"done"}],"inline_buttons":[]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "TURN_ARGS_LOG", turnArgsLog)

	payload := runScriptJSON(t, scriptPath, env,
		"start",
		"--workspace", "ws-1",
		"--assistant", "codex",
		"--prompt", "--help me choose",
	)

	if got, _ := payload["status"].(string); got == "command_error" {
		t.Fatalf("status = %q, want non-command_error for dash-prefixed prompt value", got)
	}
	argsRaw, err := os.ReadFile(turnArgsLog)
	if err != nil {
		t.Fatalf("read turn args: %v", err)
	}
	if !strings.Contains(string(argsRaw), "--prompt --help me choose") {
		t.Fatalf("turn args = %q, want dash-prefixed prompt value passed through", string(argsRaw))
	}
}

func TestOpenClawDXContinue_AutoStartAllowsDashPrefixedTextValue(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "openclaw-turn.sh")
	turnArgsLog := filepath.Join(t.TempDir(), "turn-args.log")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-} ${3:-}" in
  "workspace list --archived")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"demo","repo":"/tmp/demo","assistant":"codex"}],"error":null}'
    ;;
  "agent list --workspace")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
esac
`)
	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" > "${TURN_ARGS_LOG:?missing TURN_ARGS_LOG}"
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"started","agent_id":"agent-1","workspace_id":"ws-1","assistant":"codex","quick_actions":[],"channel":{"message":"done","chunks":["done"],"chunks_meta":[{"index":1,"total":1,"text":"done"}],"inline_buttons":[]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "TURN_ARGS_LOG", turnArgsLog)

	payload := runScriptJSON(t, scriptPath, env,
		"continue",
		"--workspace", "ws-1",
		"--auto-start",
		"--assistant", "codex",
		"--text", "--force then retry",
	)

	if got, _ := payload["status"].(string); got == "command_error" {
		t.Fatalf("status = %q, want non-command_error for dash-prefixed text value", got)
	}
	if got, _ := payload["workflow"].(string); got != "auto_start_turn" {
		t.Fatalf("workflow = %q, want %q", got, "auto_start_turn")
	}
	argsRaw, err := os.ReadFile(turnArgsLog)
	if err != nil {
		t.Fatalf("read turn args: %v", err)
	}
	if !strings.Contains(string(argsRaw), "--prompt --force then retry") {
		t.Fatalf("turn args = %q, want dash-prefixed auto-start prompt value passed through", string(argsRaw))
	}
}
