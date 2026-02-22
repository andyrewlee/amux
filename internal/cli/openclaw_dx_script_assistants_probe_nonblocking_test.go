package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXAssistants_ProbeNeedsInputIsNonBlockingWhenPassedAssistantsExist(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"demo","repo":"/tmp/demo","assistant":"codex"}],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":{},"error":null}'
    ;;
esac
`)

	for _, name := range []string{"codex", "claude", "gemini", "amp", "opencode", "droid", "cline", "agent", "pi"} {
		writeExecutable(t, filepath.Join(fakeBinDir, name), "#!/usr/bin/env bash\nset -euo pipefail\necho "+name+"\n")
	}

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
assistant=""
for ((i=1; i<=$#; i++)); do
  if [[ "${!i}" == "--assistant" ]]; then
    next=$((i+1))
    assistant="${!next}"
  fi
done
if [[ "$assistant" == "codex" ]]; then
  printf '%s' '{"ok":true,"status":"idle","overall_status":"completed","summary":"READY: codex can run non-interactive."}'
  exit 0
fi
if [[ "$assistant" == "claude" ]]; then
  printf '%s' '{"ok":true,"status":"needs_input","overall_status":"needs_input","summary":"Needs local permission confirmation."}'
  exit 0
fi
printf '%s' '{"ok":true,"status":"idle","overall_status":"completed","summary":"READY"}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)

	payload := runScriptJSON(t, scriptPath, env,
		"assistants",
		"--workspace", "ws-1",
		"--probe",
		"--limit", "2",
	)

	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["missing_count"].(float64); got != 0 {
		t.Fatalf("missing_count = %v, want 0", got)
	}
	if got, _ := data["probe_passed"].(float64); got != 1 {
		t.Fatalf("probe_passed = %v, want 1", got)
	}
	if got, _ := data["probe_needs_input"].(float64); got != 1 {
		t.Fatalf("probe_needs_input = %v, want 1", got)
	}
	nextAction, _ := payload["next_action"].(string)
	if !strings.Contains(nextAction, "Use probe-passed assistants for non-interactive mobile flows") {
		t.Fatalf("next_action = %q, want non-blocking probe guidance", nextAction)
	}
}

func TestOpenClawDXAssistants_ProbeMixedFailedAndNeedsInputIsAttention(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"demo","repo":"/tmp/demo","assistant":"codex"}],"error":null}'
    ;;
  *)
    printf '%s' '{"ok":true,"data":{},"error":null}'
    ;;
esac
`)

	for _, name := range []string{"codex", "claude", "gemini", "amp", "opencode", "droid", "cline", "agent", "pi"} {
		writeExecutable(t, filepath.Join(fakeBinDir, name), "#!/usr/bin/env bash\nset -euo pipefail\necho "+name+"\n")
	}

	writeExecutable(t, fakeTurnPath, `#!/usr/bin/env bash
set -euo pipefail
assistant=""
for ((i=1; i<=$#; i++)); do
  if [[ "${!i}" == "--assistant" ]]; then
    next=$((i+1))
    assistant="${!next}"
  fi
done
if [[ "$assistant" == "codex" ]]; then
  printf '%s' '{"ok":true,"status":"idle","overall_status":"completed","summary":"READY: codex can run non-interactive."}'
  exit 0
fi
if [[ "$assistant" == "claude" ]]; then
  printf '%s' '{"ok":true,"status":"needs_input","overall_status":"needs_input","summary":"Needs local permission confirmation."}'
  exit 0
fi
printf '%s' '{"ok":false,"status":"command_error","overall_status":"command_error","summary":"Probe failed unexpectedly."}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)

	payload := runScriptJSON(t, scriptPath, env,
		"assistants",
		"--workspace", "ws-1",
		"--probe",
		"--limit", "3",
	)

	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["probe_passed"].(float64); got != 1 {
		t.Fatalf("probe_passed = %v, want 1", got)
	}
	if got, _ := data["probe_needs_input"].(float64); got != 1 {
		t.Fatalf("probe_needs_input = %v, want 1", got)
	}
	if got, _ := data["probe_failed"].(float64); got != 1 {
		t.Fatalf("probe_failed = %v, want 1", got)
	}
}
