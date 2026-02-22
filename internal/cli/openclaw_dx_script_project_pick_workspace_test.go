package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXProjectPick_WithWorkspaceUsesWorkspaceLabel(t *testing.T) {
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
  "workspace create")
    printf '%s' '{"ok":true,"data":{"id":"ws-mobile","name":"mobile","repo":"/tmp/demo","root":"/tmp/ws-mobile","assistant":"codex"},"error":null}'
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
		"--name", "demo",
		"--workspace", "mobile",
		"--assistant", "codex",
	)

	if got, _ := payload["command"].(string); got != "project.pick" {
		t.Fatalf("command = %q, want %q", got, "project.pick")
	}
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["workspace_label"].(string); got != "ws-mobile (mobile) [project workspace]" {
		t.Fatalf("workspace_label = %q, want %q", got, "ws-mobile (mobile) [project workspace]")
	}

	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "ws-mobile (mobile) [project workspace]") {
		t.Fatalf("summary = %q, want workspace label", summary)
	}

	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	if !strings.Contains(message, "Workspace: ws-mobile (mobile) [project workspace]") {
		t.Fatalf("channel.message = %q, want workspace label", message)
	}
}
