package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXContinue_WorkspaceNotFoundReturnsCommandError(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"continue",
		"--workspace", "ws-missing",
		"--text", "resume",
		"--enter",
	)

	if got, _ := payload["command"].(string); got != "continue" {
		t.Fatalf("command = %q, want %q", got, "continue")
	}
	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "workspace not found") {
		t.Fatalf("summary = %q, want workspace not found", summary)
	}
}

func TestOpenClawDXContinue_InvalidAssistantReturnsCommandError(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"demo","repo":"/tmp/demo"}],"error":null}'
    ;;
  "agent list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"continue",
		"--workspace", "ws-1",
		"--auto-start",
		"--assistant", "not/real-assistant",
		"--text", "resume",
	)

	if got, _ := payload["command"].(string); got != "continue" {
		t.Fatalf("command = %q, want %q", got, "continue")
	}
	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "invalid assistant") {
		t.Fatalf("summary = %q, want invalid assistant", summary)
	}
}

func TestOpenClawDXContinue_WorkspaceValidationPrecedesAssistantValidation(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"continue",
		"--workspace", "ws-missing",
		"--auto-start",
		"--assistant", "not/real-assistant",
		"--text", "resume",
	)

	if got, _ := payload["command"].(string); got != "continue" {
		t.Fatalf("command = %q, want %q", got, "continue")
	}
	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "workspace not found") {
		t.Fatalf("summary = %q, want workspace not found", summary)
	}
}

func TestOpenClawDXContinue_AssistantWithoutAutoStartReturnsCommandError(t *testing.T) {
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

	payload := runScriptJSON(t, scriptPath, env,
		"continue",
		"--workspace", "ws-1",
		"--assistant", "codex",
		"--text", "resume",
	)

	if got, _ := payload["command"].(string); got != "continue" {
		t.Fatalf("command = %q, want %q", got, "continue")
	}
	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "--assistant requires --auto-start") {
		t.Fatalf("summary = %q, want --assistant requires --auto-start", summary)
	}
}
