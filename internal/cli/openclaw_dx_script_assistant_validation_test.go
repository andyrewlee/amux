package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXStart_InvalidAssistantReturnsCommandError(t *testing.T) {
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
  *)
    printf '%s' '{"ok":true,"data":{},"error":null}'
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"start",
		"--workspace", "ws-1",
		"--assistant", "not/a-real-assistant",
		"--prompt", "hello",
	)

	if got, _ := payload["command"].(string); got != "start" {
		t.Fatalf("command = %q, want %q", got, "start")
	}
	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "invalid assistant") {
		t.Fatalf("summary = %q, want invalid assistant", summary)
	}
}

func TestOpenClawDXReview_InvalidAssistantReturnsCommandError(t *testing.T) {
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
  *)
    printf '%s' '{"ok":true,"data":{},"error":null}'
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"review",
		"--workspace", "ws-1",
		"--assistant", "not/a-real-assistant",
	)

	if got, _ := payload["command"].(string); got != "review" {
		t.Fatalf("command = %q, want %q", got, "review")
	}
	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "invalid assistant") {
		t.Fatalf("summary = %q, want invalid assistant", summary)
	}
}

func TestOpenClawDXWorkflowKickoff_InvalidAssistantReturnsCommandError(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' '{"ok":true,"data":{},"error":null}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"workflow", "kickoff",
		"--name", "ws-new",
		"--project", "/tmp/example",
		"--assistant", "not/a-real-assistant",
		"--prompt", "hello",
	)

	if got, _ := payload["command"].(string); got != "workflow.kickoff" {
		t.Fatalf("command = %q, want %q", got, "workflow.kickoff")
	}
	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "invalid assistant") {
		t.Fatalf("summary = %q, want invalid assistant", summary)
	}
}

func TestOpenClawDXWorkflowDual_InvalidAssistantReturnsCommandError(t *testing.T) {
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
  *)
    printf '%s' '{"ok":true,"data":{},"error":null}'
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"workflow", "dual",
		"--workspace", "ws-1",
		"--implement-assistant", "fake/impl",
		"--review-assistant", "codex",
	)

	if got, _ := payload["command"].(string); got != "workflow.dual" {
		t.Fatalf("command = %q, want %q", got, "workflow.dual")
	}
	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want %q", got, "command_error")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "invalid assistant") {
		t.Fatalf("summary = %q, want invalid assistant", summary)
	}
}

func TestOpenClawDXStart_UnlistedAssistantPassesThrough(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "openclaw-turn.sh")

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

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"turn ok","agent_id":"agent-1","workspace_id":"ws-1","assistant":"aider","quick_actions":[],"channel":{"message":"done","chunks":["done"],"chunks_meta":[{"index":1,"total":1,"text":"done"}],"inline_buttons":[]}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)

	payload := runScriptJSON(t, scriptPath, env,
		"start",
		"--workspace", "ws-1",
		"--assistant", "aider",
		"--prompt", "hello",
	)

	if got, _ := payload["command"].(string); got != "start" {
		t.Fatalf("command = %q, want %q", got, "start")
	}
	if got, _ := payload["status"].(string); got == "command_error" {
		t.Fatalf("status = %q, expected non-command_error for unlisted assistant pass-through", got)
	}
}
