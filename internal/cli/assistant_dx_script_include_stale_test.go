package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistantDXStatus_IncludeStalePreservesArchivedWorkspaceMetadata(t *testing.T) {
	requireBinary(t, "bash")

	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "$*" in
  "workspace list --all")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "workspace list --archived --all")
    printf '%s' '{"ok":true,"data":[{"id":"ws-archived","root":"/tmp/ws-archived"}],"error":null}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[{"type":"agent","workspace_id":"ws-archived"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	t.Setenv("AMUX_BIN", fakeAmuxPath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cmdAssistantDX(&stdout, &stderr, GlobalFlags{}, []string{"status", "--include-stale"}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdAssistantDX() code = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, ExitOK, stdout.String(), stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, stdout.String())
	}
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "1 workspace(s), 1 agent session(s).") {
		t.Fatalf("summary = %q, want archived workspace metadata preserved under --include-stale", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "status --workspace ws-archived") {
		t.Fatalf("suggested_command = %q, want direct status command for archived workspace with metadata", suggested)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v, want object payload", payload["data"])
	}
	workspaces, ok := data["workspaces"].([]any)
	if !ok {
		t.Fatalf("data.workspaces = %#v, want array", data["workspaces"])
	}
	if len(workspaces) != 1 {
		t.Fatalf("data.workspaces = %#v, want archived workspace metadata surfaced", data["workspaces"])
	}
}

func TestAssistantDXStatus_IncludeStaleArchivedOnlyWorkspaceGetsStartAction(t *testing.T) {
	requireBinary(t, "bash")

	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "$*" in
  "workspace list --all")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "workspace list --archived --all")
    printf '%s' '{"ok":true,"data":[{"id":"ws-archived","root":"/tmp/ws-archived"}],"error":null}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	t.Setenv("AMUX_BIN", fakeAmuxPath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cmdAssistantDX(&stdout, &stderr, GlobalFlags{}, []string{"status", "--include-stale"}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdAssistantDX() code = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, ExitOK, stdout.String(), stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, stdout.String())
	}
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "1 workspace(s), 0 agent session(s).") {
		t.Fatalf("summary = %q, want archived-only visible workspace counted", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "start --workspace ws-archived") {
		t.Fatalf("suggested_command = %q, want start action for archived visible workspace", suggested)
	}
}

func TestAssistantDXStatus_IncludeStalePreservesWorkspaceOrderForStartSuggestion(t *testing.T) {
	requireBinary(t, "bash")

	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "$*" in
  "workspace list --all")
    printf '%s' '{"ok":true,"data":[{"id":"ws-z-live"},{"id":"ws-a-live"}],"error":null}'
    ;;
  "workspace list --archived --all")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	t.Setenv("AMUX_BIN", fakeAmuxPath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cmdAssistantDX(&stdout, &stderr, GlobalFlags{}, []string{"status", "--include-stale"}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdAssistantDX() code = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, ExitOK, stdout.String(), stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, stdout.String())
	}
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "start --workspace ws-z-live") {
		t.Fatalf("suggested_command = %q, want first workspace-list entry preserved for start suggestion", suggested)
	}
}
