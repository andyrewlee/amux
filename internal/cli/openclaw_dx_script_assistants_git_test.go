package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXAssistants_ReportsMissingFromConfig(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	readyBotPath := filepath.Join(fakeBinDir, "readybot")
	homeDir := t.TempDir()
	amuxHome := filepath.Join(homeDir, ".amux")
	if err := os.MkdirAll(amuxHome, 0o755); err != nil {
		t.Fatalf("mkdir amux home: %v", err)
	}
	configPath := filepath.Join(amuxHome, "config.json")

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
	writeExecutable(t, readyBotPath, `#!/usr/bin/env bash
set -euo pipefail
echo ready
`)
	if err := os.WriteFile(configPath, []byte(`{
  "assistants": {
    "ready": {"command": "readybot"},
    "missing": {"command": "missing-bot"}
  }
}
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "HOME", homeDir)

	payload := runScriptJSON(t, scriptPath, env, "assistants")

	if got, _ := payload["command"].(string); got != "assistants" {
		t.Fatalf("command = %q, want %q", got, "assistants")
	}
	if got, _ := payload["status"].(string); got == "needs_input" {
		t.Fatalf("status = %q, want non-blocking status when at least one assistant is ready", got)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["missing_count"].(float64); got < 1 {
		t.Fatalf("missing_count = %v, want >=1", got)
	}
	assistants, ok := data["assistants"].([]any)
	if !ok {
		t.Fatalf("assistants missing or wrong type: %T", data["assistants"])
	}
	var sawReady bool
	var sawMissing bool
	for _, raw := range assistants {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := item["name"].(string)
		status, _ := item["status"].(string)
		if name == "ready" && status == "ready" {
			sawReady = true
		}
		if name == "missing" && status == "missing" {
			sawMissing = true
		}
	}
	if !sawReady || !sawMissing {
		t.Fatalf("assistant statuses missing expected ready/missing entries: %#v", assistants)
	}
}

func TestOpenClawDXAssistants_ProbeAggregatesReadiness(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")
	passBotPath := filepath.Join(fakeBinDir, "aa-pass-bot")
	needsBotPath := filepath.Join(fakeBinDir, "ab-needs-bot")
	homeDir := t.TempDir()
	amuxHome := filepath.Join(homeDir, ".amux")
	if err := os.MkdirAll(amuxHome, 0o755); err != nil {
		t.Fatalf("mkdir amux home: %v", err)
	}
	configPath := filepath.Join(amuxHome, "config.json")

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
	writeExecutable(t, passBotPath, `#!/usr/bin/env bash
set -euo pipefail
echo pass-ready
`)
	writeExecutable(t, needsBotPath, `#!/usr/bin/env bash
set -euo pipefail
echo needs-ready
`)
	if err := os.WriteFile(configPath, []byte(`{
  "assistants": {
    "aa-pass": {"command": "aa-pass-bot"},
    "ab-needs": {"command": "ab-needs-bot"}
  }
}
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
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
if [[ "$assistant" == "aa-pass" ]]; then
  printf '%s' '{"ok":true,"status":"idle","overall_status":"completed","summary":"READY: codex objective identified."}'
  exit 0
fi
if [[ "$assistant" == "codex" ]]; then
  printf '%s' '{"ok":true,"status":"idle","overall_status":"completed","summary":"READY: codex objective identified."}'
  exit 0
fi
printf '%s' '{"ok":true,"status":"needs_input","overall_status":"needs_input","summary":"Needs local permission confirmation."}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "HOME", homeDir)
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)

	payload := runScriptJSON(t, scriptPath, env,
		"assistants",
		"--workspace", "ws-1",
		"--probe",
		"--limit", "2",
	)

	if got, _ := payload["command"].(string); got != "assistants" {
		t.Fatalf("command = %q, want %q", got, "assistants")
	}
	if got, _ := payload["status"].(string); got == "needs_input" {
		t.Fatalf("status = %q, want non-blocking status when at least one probe passed", got)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["probe_count"].(float64); got != 2 {
		t.Fatalf("probe_count = %v, want 2", got)
	}
	if got, _ := data["probe_passed"].(float64); got != 1 {
		t.Fatalf("probe_passed = %v, want 1", got)
	}
	if got, _ := data["probe_needs_input"].(float64); got != 1 {
		t.Fatalf("probe_needs_input = %v, want 1", got)
	}
	if got, _ := data["workspace_label"].(string); got != "ws-1 (demo) [project workspace]" {
		t.Fatalf("workspace_label = %q, want %q", got, "ws-1 (demo) [project workspace]")
	}
	probes, ok := data["probes"].([]any)
	if !ok || len(probes) != 2 {
		t.Fatalf("probes = %#v, want len=2", data["probes"])
	}
	var sawPassed bool
	var sawNeedsInput bool
	for _, raw := range probes {
		probe, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		result, _ := probe["result"].(string)
		if result == "passed" {
			sawPassed = true
		}
		if result == "needs_input" {
			sawNeedsInput = true
		}
	}
	if !sawPassed || !sawNeedsInput {
		t.Fatalf("probe results missing expected entries: %#v", probes)
	}

	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	if !strings.Contains(message, "Workspace: ws-1 (demo) [project workspace]") {
		t.Fatalf("channel.message = %q, want workspace label", message)
	}

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawReview bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		cmd, _ := action["command"].(string)
		if id == "review" && strings.Contains(cmd, "review --workspace ws-1 --assistant codex") {
			sawReview = true
		}
	}
	if !sawReview {
		t.Fatalf("expected workspace-specific review quick action in %#v", quickActions)
	}
}

func TestOpenClawDXAssistants_ProbePrioritizesCodexUnderLimit(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	fakeTurnPath := filepath.Join(fakeBinDir, "fake-turn.sh")
	codexPath := filepath.Join(fakeBinDir, "codex")
	ampPath := filepath.Join(fakeBinDir, "amp")

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
	writeExecutable(t, codexPath, "#!/usr/bin/env bash\nset -euo pipefail\necho codex\n")
	writeExecutable(t, ampPath, "#!/usr/bin/env bash\nset -euo pipefail\necho amp\n")

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
printf '%s' '{"ok":true,"status":"needs_input","overall_status":"needs_input","summary":"Needs local permission confirmation."}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_TURN_SCRIPT", fakeTurnPath)

	payload := runScriptJSON(t, scriptPath, env,
		"assistants",
		"--workspace", "ws-1",
		"--probe",
		"--limit", "1",
	)

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["probe_count"].(float64); got != 1 {
		t.Fatalf("probe_count = %v, want 1", got)
	}
	if got, _ := data["probe_passed"].(float64); got != 1 {
		t.Fatalf("probe_passed = %v, want 1", got)
	}
	probes, ok := data["probes"].([]any)
	if !ok || len(probes) != 1 {
		t.Fatalf("probes = %#v, want len=1", data["probes"])
	}
	firstProbe, ok := probes[0].(map[string]any)
	if !ok {
		t.Fatalf("probe[0] wrong type: %T", probes[0])
	}
	if got, _ := firstProbe["assistant"].(string); got != "codex" {
		t.Fatalf("first probed assistant = %q, want codex", got)
	}
}

func TestOpenClawDXGitShip_CommitsWorkspaceChanges(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")

	repoDir := t.TempDir()
	if out, err := exec.Command("git", "-C", repoDir, "init", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "config", "user.email", "dx@example.com").CombinedOutput(); err != nil {
		t.Fatalf("git config email: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "config", "user.name", "DX Bot").CombinedOutput(); err != nil {
		t.Fatalf("git config name: %v\n%s", err, string(out))
	}
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	if out, err := exec.Command("git", "-C", repoDir, "add", "README.md").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "commit", "-m", "initial").CombinedOutput(); err != nil {
		t.Fatalf("git commit initial: %v\n%s", err, string(out))
	}
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("modify README: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' "${FAKE_WORKSPACE_LIST_JSON:?missing FAKE_WORKSPACE_LIST_JSON}"
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	workspaceListJSON := `{"ok":true,"data":[{"id":"ws-1","name":"demo","repo":"` + repoDir + `","root":"` + repoDir + `"}],"error":null}`
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "FAKE_WORKSPACE_LIST_JSON", workspaceListJSON)

	payload := runScriptJSON(t, scriptPath, env,
		"git", "ship",
		"--workspace", "ws-1",
		"--message", "feat: update readme",
	)

	if got, _ := payload["command"].(string); got != "git.ship" {
		t.Fatalf("command = %q, want %q", got, "git.ship")
	}
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	if !strings.Contains(message, "Workspace: ws-1 (demo) [project workspace]") {
		t.Fatalf("channel.message = %q, want workspace context label", message)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	commitHash, _ := data["commit_hash"].(string)
	if strings.TrimSpace(commitHash) == "" {
		t.Fatalf("commit_hash is empty: %#v", data)
	}
	if pushed, _ := data["pushed"].(bool); pushed {
		t.Fatalf("pushed = true, expected false in local-only test")
	}

	logOut, err := exec.Command("git", "-C", repoDir, "log", "-1", "--pretty=%s").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, string(logOut))
	}
	if got := strings.TrimSpace(string(logOut)); got != "feat: update readme" {
		t.Fatalf("last commit message = %q, want %q", got, "feat: update readme")
	}
}
