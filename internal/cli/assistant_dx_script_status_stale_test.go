package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistantDXStatus_DefaultOmitsWorkspaceListAll(t *testing.T) {
	requireBinary(t, "bash")

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

	t.Setenv("AMUX_BIN", fakeAmuxPath)
	t.Setenv("AMUX_CALL_LOG", callLog)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cmdAssistantDX(&stdout, &stderr, GlobalFlags{}, []string{"status"}, "test-v1")
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
	requireBinary(t, "bash")

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

	t.Setenv("AMUX_BIN", fakeAmuxPath)
	t.Setenv("AMUX_CALL_LOG", callLog)

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

	raw, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	logText := string(raw)
	if !strings.Contains(logText, "workspace list --all") {
		t.Fatalf("workspace list should include --all with --include-stale, got:\n%s", logText)
	}
}

func TestAssistantDXStatus_IncludeStaleFallsBackWhenWorkspaceListAllIsUnsupported(t *testing.T) {
	requireBinary(t, "bash")

	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	callLog := filepath.Join(fakeBinDir, "calls.log")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "$*" in
  "workspace list --all")
    printf '%s' '{"ok":false,"error":{"code":"usage_error","message":"unknown flag: --all"}}'
    ;;
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-live"}],"error":null}'
    ;;
  "workspace list --archived --all")
    printf '%s' '{"ok":false,"error":{"code":"usage_error","message":"unknown flag: --all"}}'
    ;;
  "workspace list --archived")
    printf '%s' '{"ok":false,"error":{"code":"usage_error","message":"unknown flag: --archived"}}'
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
	t.Setenv("AMUX_CALL_LOG", callLog)

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

	raw, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	logText := string(raw)
	if !strings.Contains(logText, "workspace list --all") {
		t.Fatalf("workspace list should probe --all first, got:\n%s", logText)
	}
	if !strings.Contains(logText, "workspace list") {
		t.Fatalf("workspace list fallback missing, got:\n%s", logText)
	}
	suggested, _ := payload["suggested_command"].(string)
	if strings.Contains(suggested, "workspace list --all") {
		t.Fatalf("suggested_command = %q, want degraded visible-workspace list", suggested)
	}
	if !strings.Contains(suggested, "start --workspace ws-live") {
		t.Fatalf("suggested_command = %q, want fallback workspace to remain usable", suggested)
	}
}

func TestAssistantDXStatus_DefaultKeepsOrphanedAgentSessionsVisible(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[{"id":"ws-z-live"}],"error":null}'
    ;;
  "workspace list --archived")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[{"type":"agent","workspace_id":"ws-a-stale"},{"type":"agent","workspace_id":"ws-z-live"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "status")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	suggested, _ := payload["suggested_command"].(string)
	if strings.Contains(suggested, "ws-a-stale") {
		t.Fatalf("suggested_command = %q, want metadata-backed workspace to be preferred when one is available", suggested)
	}
	if !strings.Contains(suggested, "ws-z-live") {
		t.Fatalf("suggested_command = %q, want visible workspace status command", suggested)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "2 workspace(s), 2 agent session(s).") {
		t.Fatalf("summary = %q, want orphaned live agent sessions preserved in surfaced totals", summary)
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
		t.Fatalf("data.sessions len = %d, want both live agent sessions", len(sessions))
	}
	seen := map[string]bool{}
	for i, raw := range sessions {
		session, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("data.sessions[%d] = %#v, want object", i, raw)
		}
		if got, _ := session["workspace_id"].(string); got != "" {
			seen[got] = true
		}
	}
	if !seen["ws-a-stale"] || !seen["ws-z-live"] {
		t.Fatalf("data.sessions = %#v, want both workspace IDs surfaced", sessions)
	}
}

func TestAssistantDXStatus_DefaultSurfacesAgentSessionWhenWorkspaceMetadataIsMissing(t *testing.T) {
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

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "status")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "1 workspace(s), 1 agent session(s).") {
		t.Fatalf("summary = %q, want orphaned live workspace preserved in surfaced totals", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "assistant-dx.sh workspace list --all") {
		t.Fatalf("suggested_command = %q, want orphaned live agent fallback to workspace list", suggested)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v, want object payload", payload["data"])
	}
	sessions, ok := data["sessions"].([]any)
	if !ok {
		t.Fatalf("data.sessions = %#v, want array", data["sessions"])
	}
	if len(sessions) != 1 {
		t.Fatalf("data.sessions len = %d, want orphaned live agent session retained", len(sessions))
	}
}

func TestAssistantDXStatus_DefaultIncludesArchivedAgentSessionsInSuggestion(t *testing.T) {
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
  "workspace list --archived")
    printf '%s' '{"ok":true,"data":[{"id":"ws-archived"}],"error":null}'
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[{"type":"agent","workspace_id":"ws-archived"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "status")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "1 workspace(s), 1 agent session(s).") {
		t.Fatalf("summary = %q, want archived live workspace to be included in surfaced totals", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "ws-archived") {
		t.Fatalf("suggested_command = %q, want archived live agent workspace suggestion", suggested)
	}
}

func TestAssistantDXStatus_DefaultIgnoresArchivedWorkspacesWithoutLiveAgentSessions(t *testing.T) {
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
  "workspace list --archived")
    printf '%s' '{"ok":true,"data":[{"id":"ws-archived"}],"error":null}'
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

	payload := runScriptJSON(t, scriptPath, env, "status")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "0 workspace(s), 0 agent session(s).") {
		t.Fatalf("summary = %q, want inactive archived workspaces excluded from default totals", summary)
	}
}

func TestAssistantDXStatus_ArchivedProbeFailureStillEmitsSingleSuccessJSON(t *testing.T) {
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
    printf '%s' '{"ok":false,"error":{"code":"unsupported","message":"flag provided but not defined: --archived"}}'
    exit 1
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[{"type":"agent","workspace_id":"ws-live"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := withIsolatedAssistantDXContext(t, withEnv(os.Environ(), "PATH", fakeBinDir+":"+os.Getenv("PATH")))
	cmd := exec.Command(scriptPath, "status")
	cmd.Env = env
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s status failed: %v\nstdout:\n%s\nstderr:\n%s", scriptPath, err, stdout.String(), stderr.String())
	}

	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	var payload map[string]any
	if err := dec.Decode(&payload); err != nil {
		t.Fatalf("decode first json object: %v\nraw: %s", err, stdout.String())
	}
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	var extra map[string]any
	if err := dec.Decode(&extra); err != io.EOF {
		t.Fatalf("expected single json object, got extra payload %#v (err=%v)\nraw: %s", extra, err, stdout.String())
	}
}

func TestAssistantDXStatus_ArchivedProbeUnsupportedStillKeepsArchivedAgentSessions(t *testing.T) {
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
  "workspace list --archived")
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

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "status")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "1 workspace(s), 1 agent session(s).") {
		t.Fatalf("summary = %q, want archived live workspace preserved in surfaced totals when --archived is unsupported", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if strings.Contains(suggested, "status --workspace ws-archived") {
		t.Fatalf("suggested_command = %q, want no broken status suggestion for unverifiable archived session when --archived is unsupported", suggested)
	}
	if !strings.Contains(suggested, "workspace list --all") {
		t.Fatalf("suggested_command = %q, want workspace listing fallback when --archived is unsupported", suggested)
	}
}

func TestAssistantDXStatus_ArchivedProbeErrorStillKeepsArchivedAgentSessions(t *testing.T) {
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
  "workspace list --archived")
    printf '%s\n' 'failed to open workspace registry' >&2
    exit 1
    ;;
  "session list")
    printf '%s' '{"ok":true,"data":[{"type":"agent","workspace_id":"ws-archived"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env, "status")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "1 workspace(s), 1 agent session(s).") {
		t.Fatalf("summary = %q, want archived live workspace preserved in surfaced totals when archived lookup errors", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if strings.Contains(suggested, "status --workspace ws-archived") {
		t.Fatalf("suggested_command = %q, want no broken status suggestion for unverifiable archived session when archived lookup errors", suggested)
	}
	if !strings.Contains(suggested, "workspace list --all") {
		t.Fatalf("suggested_command = %q, want workspace listing fallback when archived lookup errors", suggested)
	}
}
