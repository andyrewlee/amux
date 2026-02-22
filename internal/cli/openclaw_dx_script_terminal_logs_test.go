package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXTerminalLogs_DetectsNgrokAuthLimit(t *testing.T) {
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
if [[ "${1:-}" == "workspace" && "${2:-}" == "list" && "${3:-}" == "--archived" ]]; then
  printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"mobile","repo":"/tmp/demo","scope":"project"}],"error":null}'
  exit 0
fi
if [[ "${1:-}" == "terminal" && "${2:-}" == "logs" ]]; then
  printf '%s' '{"ok":true,"data":{"workspace_id":"ws-1","content":"ERROR: authentication failed: Your account is limited to 1 simultaneous ngrok agent sessions.\nERROR: ERR_NGROK_108\n"}}'
  exit 0
fi
printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"terminal", "logs",
		"--workspace", "ws-1",
		"--lines", "80",
	)

	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "ngrok authentication/session issue") {
		t.Fatalf("summary = %q, want ngrok auth/session guidance", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if strings.Contains(suggested, "pkill ngrok") || !strings.Contains(suggested, "ngrok http 3000") {
		t.Fatalf("suggested_command = %q, want non-destructive ngrok retry command", suggested)
	}

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	diagnostic, ok := data["diagnostic"].(map[string]any)
	if !ok {
		t.Fatalf("diagnostic missing or wrong type: %T", data["diagnostic"])
	}
	if got, _ := diagnostic["reason"].(string); got != "ngrok_auth_session_limit" {
		t.Fatalf("diagnostic.reason = %q, want %q", got, "ngrok_auth_session_limit")
	}

	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	if !strings.Contains(message, "Detected issue: ngrok reported authentication/session limit") {
		t.Fatalf("channel.message = %q, want ngrok detected issue hint", message)
	}

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawRetryNgrok bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		cmd, _ := action["command"].(string)
		if id == "retry_ngrok" && !strings.Contains(cmd, "pkill ngrok") && strings.Contains(cmd, "ngrok http 3000") {
			sawRetryNgrok = true
			break
		}
	}
	if !sawRetryNgrok {
		t.Fatalf("expected retry_ngrok quick action in %#v", quickActions)
	}
}

func TestOpenClawDXTerminalLogs_DetectsMissingPackageManifest(t *testing.T) {
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
if [[ "${1:-}" == "workspace" && "${2:-}" == "list" && "${3:-}" == "--archived" ]]; then
  printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"mobile","repo":"/tmp/demo","scope":"project"}],"error":null}'
  exit 0
fi
if [[ "${1:-}" == "terminal" && "${2:-}" == "logs" ]]; then
  printf '%s' '{"ok":true,"data":{"workspace_id":"ws-1","content":"ERR_PNPM_NO_IMPORTER_MANIFEST_FOUND No package.json was found in the current workspace."}}'
  exit 0
fi
printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"terminal", "logs",
		"--workspace", "ws-1",
		"--lines", "120",
	)

	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "missing package manifest") {
		t.Fatalf("summary = %q, want package-manifest guidance", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "pwd && ls -la") {
		t.Fatalf("suggested_command = %q, want workspace inspection command", suggested)
	}

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	diagnostic, ok := data["diagnostic"].(map[string]any)
	if !ok {
		t.Fatalf("diagnostic missing or wrong type: %T", data["diagnostic"])
	}
	if got, _ := diagnostic["reason"].(string); got != "missing_package_manifest" {
		t.Fatalf("diagnostic.reason = %q, want %q", got, "missing_package_manifest")
	}
}

func TestOpenClawDXTerminalLogs_DetectsPortInUse(t *testing.T) {
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
if [[ "${1:-}" == "workspace" && "${2:-}" == "list" && "${3:-}" == "--archived" ]]; then
  printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"mobile","repo":"/tmp/demo","scope":"project"}],"error":null}'
  exit 0
fi
if [[ "${1:-}" == "terminal" && "${2:-}" == "logs" ]]; then
  printf '%s' '{"ok":true,"data":{"workspace_id":"ws-1","content":"Error: listen EADDRINUSE: address already in use :::3000"}}'
  exit 0
fi
printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"terminal", "logs",
		"--workspace", "ws-1",
		"--lines", "80",
	)

	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "port already in use") {
		t.Fatalf("summary = %q, want port-in-use guidance", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "--port 3001") {
		t.Fatalf("suggested_command = %q, want alternate port command", suggested)
	}

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	diagnostic, ok := data["diagnostic"].(map[string]any)
	if !ok {
		t.Fatalf("diagnostic missing or wrong type: %T", data["diagnostic"])
	}
	if got, _ := diagnostic["reason"].(string); got != "port_in_use" {
		t.Fatalf("diagnostic.reason = %q, want %q", got, "port_in_use")
	}
}

func TestOpenClawDXTerminalLogs_DetectsLocalServerUnreachable(t *testing.T) {
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
if [[ "${1:-}" == "workspace" && "${2:-}" == "list" && "${3:-}" == "--archived" ]]; then
  printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"mobile","repo":"/tmp/demo","scope":"project"}],"error":null}'
  exit 0
fi
if [[ "${1:-}" == "terminal" && "${2:-}" == "logs" ]]; then
  printf '%s' '{"ok":true,"data":{"workspace_id":"ws-1","content":"ngrok failed to connect to localhost:3000: connection refused"}}'
  exit 0
fi
printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"terminal", "logs",
		"--workspace", "ws-1",
		"--lines", "80",
	)

	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "tunnel/local-server connectivity issue") {
		t.Fatalf("summary = %q, want local-server-unreachable guidance", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "terminal preset --workspace ws-1 --kind nextjs --port 3000") {
		t.Fatalf("suggested_command = %q, want start-server preset command", suggested)
	}

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	diagnostic, ok := data["diagnostic"].(map[string]any)
	if !ok {
		t.Fatalf("diagnostic missing or wrong type: %T", data["diagnostic"])
	}
	if got, _ := diagnostic["reason"].(string); got != "local_server_unreachable" {
		t.Fatalf("diagnostic.reason = %q, want %q", got, "local_server_unreachable")
	}

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawRetryNgrok bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		if id == "retry_ngrok" {
			sawRetryNgrok = true
			break
		}
	}
	if !sawRetryNgrok {
		t.Fatalf("expected retry_ngrok quick action in %#v", quickActions)
	}
}
