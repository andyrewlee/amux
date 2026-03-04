package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistantDXStatus_DefaultOmitsWorkspaceListAll(t *testing.T) {
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
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-a"}],"error":null}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "AMUX_CALL_LOG", callLog)
	payload := runScriptJSON(t, scriptPath, env, "status")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}

	raw, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	logText := string(raw)
	if strings.Contains(logText, "workspace list --all") {
		t.Fatalf("workspace list should not include --all by default, got:\n%s", logText)
	}
	if !strings.Contains(logText, "workspace list") {
		t.Fatalf("expected workspace list call, got:\n%s", logText)
	}
}

func TestAssistantDXStatus_IncludeStaleUsesWorkspaceListAll(t *testing.T) {
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
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-a"}],"error":null}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "AMUX_CALL_LOG", callLog)
	payload := runScriptJSON(t, scriptPath, env, "status", "--include-stale")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}

	raw, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	logText := string(raw)
	if !strings.Contains(logText, "workspace list --all") {
		t.Fatalf("workspace list should include --all with --include-stale, got:\n%s", logText)
	}
}
