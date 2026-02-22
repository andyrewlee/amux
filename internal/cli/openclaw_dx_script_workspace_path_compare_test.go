package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXWorkspaceCreate_AllowsEquivalentProjectPathRepresentations(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	calledFile := filepath.Join(fakeBinDir, "workspace-create-called.txt")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"parent-ws","name":"feature","repo":"/tmp/demo","assistant":"codex"}],"error":null}'
    ;;
  "workspace create")
    printf 'called' > "${CALLED_FILE:?missing CALLED_FILE}"
    printf '%s' '{"ok":true,"data":{"id":"ws-nested","name":"feature.refactor","repo":"/tmp/demo","root":"/tmp/ws-nested","assistant":"codex"},"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "CALLED_FILE", calledFile)

	payload := runScriptJSON(t, scriptPath, env,
		"workspace", "create",
		"--name", "refactor",
		"--from-workspace", "parent-ws",
		"--project", "/tmp/demo/",
		"--scope", "nested",
	)

	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Nested workspace ready") {
		t.Fatalf("summary = %q, want nested workspace creation success", summary)
	}
	calledRaw, err := os.ReadFile(calledFile)
	if err != nil {
		t.Fatalf("read called file: %v", err)
	}
	if got := strings.TrimSpace(string(calledRaw)); got != "called" {
		t.Fatalf("workspace create call marker = %q, want %q", got, "called")
	}
}
