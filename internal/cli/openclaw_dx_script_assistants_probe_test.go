package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXAssistants_ProbeWorkspaceNotFoundReturnsCommandError(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	homeDir := t.TempDir()
	amuxHome := filepath.Join(homeDir, ".amux")
	if err := os.MkdirAll(amuxHome, 0o755); err != nil {
		t.Fatalf("mkdir amux home: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":{},"error":null}'
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "HOME", homeDir)

	payload := runScriptJSON(t, scriptPath, env,
		"assistants",
		"--workspace", "ws-missing",
		"--probe",
	)

	if got, _ := payload["command"].(string); got != "assistants" {
		t.Fatalf("command = %q, want %q", got, "assistants")
	}
	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "workspace not found") {
		t.Fatalf("summary = %q, want workspace not found", summary)
	}
}

func TestOpenClawDXAssistants_ProbePrefersProbePassedAssistant(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")
	claudeBotPath := filepath.Join(fakeBinDir, "claude-ready-bot")
	codexBotPath := filepath.Join(fakeBinDir, "codex-ready-bot")
	homeDir := t.TempDir()
	amuxHome := filepath.Join(homeDir, ".amux")
	if err := os.MkdirAll(amuxHome, 0o755); err != nil {
		t.Fatalf("mkdir amux home: %v", err)
	}
	configPath := filepath.Join(amuxHome, "config.json")

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
    printf '%s' '{"ok":true,"data":{},"error":null}'
    ;;
esac
`)
	writeExecutable(t, claudeBotPath, `#!/usr/bin/env bash
set -euo pipefail
echo ready
`)
	writeExecutable(t, codexBotPath, `#!/usr/bin/env bash
set -euo pipefail
echo ready
`)
	if err := os.WriteFile(configPath, []byte(`{
  "assistants": {
    "claude": {"command": "claude-ready-bot"},
    "codex": {"command": "codex-ready-bot"}
  }
}
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

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
  printf '%s' '{"ok":true,"status":"needs_input","overall_status":"needs_input","summary":"Needs local permission choice."}'
  exit 0
fi
printf '%s' '{"ok":true,"status":"idle","overall_status":"completed","summary":"READY: codex can proceed."}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "HOME", homeDir)
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)

	payload := runScriptJSON(t, scriptPath, env,
		"assistants",
		"--workspace", "ws-1",
		"--probe",
		"--limit", "9",
	)

	if got, _ := payload["command"].(string); got != "assistants" {
		t.Fatalf("command = %q, want %q", got, "assistants")
	}
	if got, _ := payload["status"].(string); got == "needs_input" {
		t.Fatalf("status = %q, want non-blocking status when a probe-passed assistant exists", got)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "--assistant codex") {
		t.Fatalf("suggested_command = %q, want codex start recommendation", suggested)
	}
	if strings.Contains(suggested, "workflow dual") {
		t.Fatalf("suggested_command = %q, expected no dual workflow when claude probe needs input", suggested)
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawStartReady bool
	var sawDual bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		if id == "start_ready" {
			sawStartReady = true
		}
		if id == "dual" {
			sawDual = true
		}
	}
	if !sawStartReady {
		t.Fatalf("expected start_ready quick action in %#v", quickActions)
	}
	if sawDual {
		t.Fatalf("did not expect dual quick action when claude probe needs input: %#v", quickActions)
	}
}
