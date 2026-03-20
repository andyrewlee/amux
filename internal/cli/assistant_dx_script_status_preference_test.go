package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistantDXStatus_DefaultPrefersLiveWorkspaceSuggestionOverArchivedSessionOrder(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "$*" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-live"}],"error":null}'
    ;;
  "workspace list --archived")
    printf '%s' '{"ok":true,"data":[{"id":"ws-archived"}],"error":null}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[{"type":"agent","workspace_id":"ws-archived"},{"type":"agent","workspace_id":"ws-live"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := withEnv(os.Environ(), "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "status")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "ws-live") {
		t.Fatalf("suggested_command = %q, want live workspace suggestion", suggested)
	}
	if strings.Contains(suggested, "ws-archived") {
		t.Fatalf("suggested_command = %q, want archived workspace excluded when live workspace is available", suggested)
	}
}

func TestAssistantDXStatus_DefaultDoesNotTargetUnrelatedLiveWorkspaceForOrphanedSession(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "$*" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-live"}],"error":null}'
    ;;
  "workspace list --archived")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[{"type":"agent","workspace_id":"ws-orphaned"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := withEnv(os.Environ(), "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "status")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	suggested, _ := payload["suggested_command"].(string)
	if strings.Contains(suggested, "status --workspace ws-live") {
		t.Fatalf("suggested_command = %q, want unrelated live workspace excluded for orphaned agent session", suggested)
	}
	if !strings.Contains(suggested, "workspace list --all") {
		t.Fatalf("suggested_command = %q, want workspace list fallback for orphaned agent session", suggested)
	}
}

func TestAssistantDXStatus_ArchivedProbeFailureFallsBackToSupportedWorkspaceListForUnknownAgentSession(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "$*" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-live"}],"error":null}'
    ;;
  "workspace list --archived --all")
    printf '%s\n' 'flag provided but not defined: --archived' >&2
    exit 2
    ;;
  "workspace list --archived")
    printf '%s\n' 'flag provided but not defined: --archived' >&2
    exit 2
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[{"type":"agent","workspace_id":"ws-orphaned"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := withEnv(os.Environ(), "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "status")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	suggested, _ := payload["suggested_command"].(string)
	if strings.Contains(suggested, "status --workspace ws-orphaned") {
		t.Fatalf("suggested_command = %q, want no broken status command for unknown agent workspace", suggested)
	}
	if !strings.Contains(suggested, "workspace list") || strings.Contains(suggested, "workspace list --all") {
		t.Fatalf("suggested_command = %q, want supported workspace-list fallback for unknown agent workspace", suggested)
	}
}

func TestAssistantDXStatus_PreservesNonAgentSessionsInPayload(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "$*" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-live"}],"error":null}'
    ;;
  "workspace list --archived")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[{"type":"agent","workspace_id":"ws-live","id":"sess-agent"},{"type":"terminal","workspace_id":"ws-live","id":"sess-term"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := withEnv(os.Environ(), "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "status")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v, want object payload", payload["data"])
	}
	sessions, ok := data["sessions"].([]any)
	if !ok {
		t.Fatalf("data.sessions = %#v, want array", data["sessions"])
	}
	if len(sessions) != 2 {
		t.Fatalf("data.sessions len = %d, want both agent and terminal sessions", len(sessions))
	}
	var sawTerminal bool
	for _, raw := range sessions {
		session, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("session = %#v, want object", raw)
		}
		if session["type"] == "terminal" {
			sawTerminal = true
		}
	}
	if !sawTerminal {
		t.Fatalf("data.sessions = %#v, want terminal session preserved", sessions)
	}
}
