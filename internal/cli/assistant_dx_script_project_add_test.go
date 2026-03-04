package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistantDXProjectAdd_UsesPositionalPathForAmuxCLI(t *testing.T) {
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
  "project add")
    if [[ "${3:-}" == "--path" ]]; then
      printf '%s' '{"ok":false,"error":{"code":"usage_error","message":"unexpected --path flag"}}'
      exit 2
    fi
    printf '{"ok":true,"data":{"name":"repo","path":"%s"},"error":null}' "${3:-}"
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "AMUX_CALL_LOG", callLog)

	payload := runScriptJSON(t, scriptPath, env, "project", "add", "--path", "/tmp/repo")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}

	raw, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	logText := string(raw)
	if strings.Contains(logText, "project add --path") {
		t.Fatalf("project add call should not use --path flag, got:\n%s", logText)
	}
	if !strings.Contains(logText, "project add /tmp/repo") {
		t.Fatalf("project add call should pass positional path, got:\n%s", logText)
	}
}
