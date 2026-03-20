package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistantDXStatus_ArchivedProbeUnsupportedUsesSessionListForArchivedOnlyAgent(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "workspace list --archived --all")
    printf '%s\n' 'flag provided but not defined: --archived' >&2
    exit 2
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[{"type":"agent","workspace_id":"ws-archived"}],"error":null}'
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
	if !strings.Contains(suggested, "session list") {
		t.Fatalf("suggested_command = %q, want session-list fallback for archived-only agent on old CLI", suggested)
	}
	if strings.Contains(suggested, "workspace list --all") {
		t.Fatalf("suggested_command = %q, want no workspace-list dead end for archived-only agent on old CLI", suggested)
	}
}

func TestAssistantDXStatus_DefaultExcludesHiddenLiveStaleWorkspaceFromPrimarySuggestion(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "workspace list --archived --all")
    printf '%s' '{"ok":true,"data":[{"id":"ws-stale-live","archived":false,"root":"/tmp/ws-stale-live"}],"error":null}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[{"type":"agent","workspace_id":"ws-stale-live"}],"error":null}'
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
	if strings.Contains(suggested, "status --workspace ws-stale-live") {
		t.Fatalf("suggested_command = %q, want hidden stale live workspace excluded from primary status suggestion", suggested)
	}
	if !strings.Contains(suggested, "workspace list --all") {
		t.Fatalf("suggested_command = %q, want workspace-list fallback for hidden stale live workspace", suggested)
	}
}

func TestAssistantDXStatus_ArchivedProbeFallsBackToArchivedWithoutAll(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "workspace list --archived --all")
    printf '%s\n' 'flag provided but not defined: --all' >&2
    exit 2
    ;;
  "workspace list --archived")
    printf '%s' '{"ok":true,"data":[{"id":"ws-archived","archived":true,"root":"/tmp/ws-archived"}],"error":null}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[{"type":"agent","workspace_id":"ws-archived"}],"error":null}'
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
	if !strings.Contains(suggested, "status --workspace ws-archived") {
		t.Fatalf("suggested_command = %q, want archived status suggestion via --archived fallback", suggested)
	}
	if strings.Contains(suggested, "session list") {
		t.Fatalf("suggested_command = %q, want no session-list fallback when archived metadata is available", suggested)
	}
}

func TestAssistantDXStatus_GenericUnsupportedArchivedProbeUsesSessionListFallback(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "workspace list --archived --all")
    printf '%s' '{"ok":false,"error":{"code":"unsupported","message":"unsupported"}}'
    ;;
  "workspace list --archived")
    printf '%s' '{"ok":false,"error":{"code":"unsupported","message":"unsupported"}}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[{"type":"agent","workspace_id":"ws-archived"}],"error":null}'
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
	if !strings.Contains(suggested, "session list") {
		t.Fatalf("suggested_command = %q, want session-list fallback for generic unsupported archived probe", suggested)
	}
	if strings.Contains(suggested, "workspace list --all") {
		t.Fatalf("suggested_command = %q, want no workspace-list dead end for generic unsupported archived probe", suggested)
	}
}

func TestAssistantDXStatus_UnsupportedAllFallbackErrorUsesSupportedWorkspaceList(t *testing.T) {
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
    printf '%s\n' 'flag provided but not defined: --all' >&2
    exit 2
    ;;
  "workspace list --archived")
    printf '%s' '{"ok":false,"error":{"code":"registry_read_failed","message":"registry read failed"}}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[{"type":"agent","workspace_id":"ws-archived"}],"error":null}'
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
	if !strings.Contains(suggested, "workspace list") || strings.Contains(suggested, "workspace list --all") {
		t.Fatalf("suggested_command = %q, want supported visible-workspace fallback after unsupported --all plus archived-probe error", suggested)
	}
}
