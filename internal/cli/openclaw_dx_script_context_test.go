package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXProjectPick_NameSupportsDisambiguation(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[{"name":"api-core","path":"/tmp/api-core"},{"name":"api-gateway","path":"/tmp/api-gateway"},{"name":"mobile","path":"/tmp/mobile"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"project", "pick",
		"--name", "api",
	)

	if got, _ := payload["command"].(string); got != "project.pick" {
		t.Fatalf("command = %q, want %q", got, "project.pick")
	}
	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	if got, _ := payload["ok"].(bool); got {
		t.Fatalf("ok = true, want false when disambiguation is required")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	matches, ok := data["matches"].([]any)
	if !ok || len(matches) != 2 {
		t.Fatalf("matches = %#v, want len=2", data["matches"])
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawPick1 bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		if id == "pick_1" {
			sawPick1 = true
			break
		}
	}
	if !sawPick1 {
		t.Fatalf("expected pick_1 quick action in %#v", quickActions)
	}
}

func TestOpenClawDXGuide_RecommendsReplyWhenAgentNeedsInput(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"mobile","repo":"/tmp/demo","root":"/tmp/ws-1","assistant":"codex"}],"error":null}'
    ;;
  "agent list")
    printf '%s' '{"ok":true,"data":[{"agent_id":"agent-1","session_name":"sess-1","workspace_id":"ws-1"}],"error":null}'
    ;;
  "terminal list")
    printf '%s' '{"ok":true,"data":[{"workspace_id":"ws-1","session_name":"term-1"}],"error":null}'
    ;;
  "agent capture")
    printf '%s' '{"ok":true,"data":{"status":"captured","summary":"Need user choice before proceeding.","needs_input":true,"input_hint":"Choose migration path A or B."},"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"guide",
		"--workspace", "ws-1",
	)

	if got, _ := payload["command"].(string); got != "guide" {
		t.Fatalf("command = %q, want %q", got, "guide")
	}
	if got, _ := payload["status"].(string); got != "needs_input" {
		t.Fatalf("status = %q, want %q", got, "needs_input")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["stage"].(string); got != "reply_agent" {
		t.Fatalf("stage = %q, want %q", got, "reply_agent")
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "continue --agent agent-1") {
		t.Fatalf("suggested_command = %q, want continue command", suggested)
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawReply bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		if id == "reply" {
			sawReply = true
			break
		}
	}
	if !sawReply {
		t.Fatalf("expected reply quick action in %#v", quickActions)
	}
}

func TestOpenClawDXStart_UsesSiblingTurnScriptWhenInvokedOutsideRepoRoot(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	sourceScriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	src, err := os.ReadFile(sourceScriptPath)
	if err != nil {
		t.Fatalf("read source script: %v", err)
	}

	scriptDir := t.TempDir()
	runDir := t.TempDir()
	copiedScriptPath := filepath.Join(scriptDir, "openclaw-dx.sh")
	if err := os.WriteFile(copiedScriptPath, src, 0o755); err != nil {
		t.Fatalf("write copied script: %v", err)
	}

	argsLog := filepath.Join(scriptDir, "turn-args.log")
	fakeTurnPath := filepath.Join(scriptDir, "openclaw-turn.sh")
	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" > "${TURN_ARGS_LOG:?missing TURN_ARGS_LOG}"
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"sibling turn script used","agent_id":"agent-1","workspace_id":"ws-1","quick_actions":[],"channel":{"message":"done","chunks":["done"],"chunks_meta":[{"index":1,"total":1,"text":"done"}]}}'
`)

	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
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

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "TURN_ARGS_LOG", argsLog)

	payload := runScriptJSONInDir(t, copiedScriptPath, runDir, env,
		"start",
		"--workspace", "ws-1",
		"--assistant", "codex",
		"--prompt", "hello",
	)

	if got, _ := payload["command"].(string); got != "start" {
		t.Fatalf("command = %q, want %q", got, "start")
	}
	if got, _ := payload["status"].(string); got != "idle" {
		t.Fatalf("status = %q, want %q", got, "idle")
	}
	argsRaw, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("read args log: %v", err)
	}
	args := strings.TrimSpace(string(argsRaw))
	if !strings.Contains(args, "run") || !strings.Contains(args, "--workspace ws-1") {
		t.Fatalf("turn args = %q, expected run/workspace", args)
	}
}

func TestOpenClawDXStart_UsesContextWorkspaceAndAssistant(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")
	turnArgsPath := filepath.Join(fakeBinDir, "turn-args.log")
	contextPath := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(contextPath, []byte(`{"workspace":{"id":"ws-context","assistant":"claude"}}`), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-context","name":"demo","repo":"/tmp/demo"}],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":{},"error":null}'
    ;;
esac
`)

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" > "${TURN_ARGS_PATH:?missing TURN_ARGS_PATH}"
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"turn complete","agent_id":"agent-ctx","workspace_id":"ws-context","assistant":"claude","quick_actions":[],"channel":{"message":"done","chunks":["done"],"chunks_meta":[{"index":1,"total":1,"text":"done"}],"inline_buttons":[]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "TURN_ARGS_PATH", turnArgsPath)
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)
	env = withEnv(env, "OPENCLAW_DX_CONTEXT_FILE", contextPath)

	payload := runScriptJSON(t, scriptPath, env,
		"start",
		"--prompt", "Continue work",
	)

	if got, _ := payload["command"].(string); got != "start" {
		t.Fatalf("command = %q, want %q", got, "start")
	}
	if got, _ := payload["assistant"].(string); got != "claude" {
		t.Fatalf("assistant = %q, want claude", got)
	}

	turnArgsRaw, err := os.ReadFile(turnArgsPath)
	if err != nil {
		t.Fatalf("read turn args: %v", err)
	}
	turnArgs := string(turnArgsRaw)
	if !strings.Contains(turnArgs, "--workspace ws-context") {
		t.Fatalf("turn args missing context workspace: %s", turnArgs)
	}
	if !strings.Contains(turnArgs, "--assistant claude") {
		t.Fatalf("turn args missing context assistant: %s", turnArgs)
	}
}
