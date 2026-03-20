package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdAssistantDXGitShipPushFailureKeepsCommitSuccess(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoRoot := t.TempDir()
	runGit(t, repoRoot, "init")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "amux-test")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("before\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	runGit(t, repoRoot, "add", "README.md")
	runGit(t, repoRoot, "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("after\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-} ${3:-}" in
  "workspace list --all")
    printf '%s' "${FAKE_WORKSPACE_LIST_JSON:?missing FAKE_WORKSPACE_LIST_JSON}"
    ;;
  *)
    printf '%s' '{"ok":false,"error":{"code":"unexpected","message":"unexpected args"}}'
    exit 2
    ;;
esac
`)

	t.Setenv("AMUX_BIN", fakeAmuxPath)
	t.Setenv("FAKE_WORKSPACE_LIST_JSON", `{"ok":true,"data":[{"id":"ws-1","root":"`+repoRoot+`"}],"error":null}`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cmdAssistantDX(&stdout, &stderr, GlobalFlags{}, []string{"git", "ship", "--workspace", "ws-1", "--push"}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdAssistantDX() code = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, ExitOK, stdout.String(), stderr.String())
	}

	var payload assistantDXPayload
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("payload.OK = false, want true: %#v", payload)
	}
	if payload.Status != "attention" {
		t.Fatalf("payload.Status = %q, want attention", payload.Status)
	}

	data, ok := payload.Data.(map[string]any)
	if !ok {
		t.Fatalf("payload.Data type = %T, want map[string]any", payload.Data)
	}
	if got, _ := data["pushed"].(bool); got {
		t.Fatalf("data.pushed = true, want false")
	}
	pushError, _ := data["push_error"].(string)
	if strings.TrimSpace(pushError) == "" {
		t.Fatalf("data.push_error is empty, want git push failure details")
	}

	if got := runGitOutput(t, repoRoot, "rev-list", "--count", "HEAD"); got != "2" {
		t.Fatalf("git rev-list --count HEAD = %q, want 2", got)
	}
	if got := runGitOutput(t, repoRoot, "log", "-1", "--pretty=%s"); got != "chore(amux): update ws-1" {
		t.Fatalf("git log -1 --pretty=%%s = %q, want default ship commit", got)
	}
}

func TestNewAssistantDXInvokerPrefersInternalProcessOverPATHBinary(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "amux.log")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
printf '%s' '{"ok":true,"data":{"source":"external"},"error":null}'
`)

	t.Setenv("PATH", fakeBinDir+":"+os.Getenv("PATH"))
	t.Setenv("AMUX_BIN", "")
	t.Setenv("AMUX_CALL_LOG", logPath)

	inv := newAssistantDXInvoker("test-v1")
	if inv.useExternal {
		t.Fatalf("useExternal = true, want false when only PATH amux is present")
	}

	result := inv.call("unknown")
	if result.Envelope == nil || result.Envelope.OK {
		t.Fatalf("call() envelope = %#v, want internal error envelope", result.Envelope)
	}
	if result.ExitCode != ExitUsage {
		t.Fatalf("call() exit code = %d, want %d", result.ExitCode, ExitUsage)
	}

	if data, err := os.ReadFile(logPath); err == nil && strings.TrimSpace(string(data)) != "" {
		t.Fatalf("external amux was invoked unexpectedly:\n%s", string(data))
	}
}

func TestNewAssistantDXInvokerRespectsExplicitAMUXBIN(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "amux.log")
	fakeAmuxPath := filepath.Join(t.TempDir(), "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
printf '%s' '{"ok":true,"data":{"source":"external"},"error":null}'
`)

	t.Setenv("AMUX_BIN", fakeAmuxPath)
	t.Setenv("AMUX_CALL_LOG", logPath)

	inv := newAssistantDXInvoker("test-v1")
	if !inv.useExternal {
		t.Fatalf("useExternal = false, want true when AMUX_BIN is set")
	}

	result := inv.call("project", "list")
	if result.Envelope == nil || !result.Envelope.OK {
		t.Fatalf("call() envelope = %#v, want external success envelope", result.Envelope)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", logPath, err)
	}
	if !strings.Contains(string(raw), "--json project list") {
		t.Fatalf("external amux log = %q, want --json project list", string(raw))
	}
}

func TestNewAssistantDXInvokerForcesInternalWhenRequested(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "amux.log")
	fakeAmuxPath := filepath.Join(t.TempDir(), "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
printf '%s' '{"ok":true,"data":{"source":"external"},"error":null}'
`)

	t.Setenv("AMUX_BIN", fakeAmuxPath)
	t.Setenv("AMUX_CALL_LOG", logPath)
	t.Setenv("AMUX_ASSISTANT_DX_FORCE_INTERNAL", "true")

	inv := newAssistantDXInvoker("test-v1")
	if inv.useExternal {
		t.Fatalf("useExternal = true, want false when internal DX execution is forced")
	}

	result := inv.call("unknown")
	if result.Envelope == nil || result.Envelope.OK {
		t.Fatalf("call() envelope = %#v, want internal error envelope", result.Envelope)
	}
	if result.ExitCode != ExitUsage {
		t.Fatalf("call() exit code = %d, want %d", result.ExitCode, ExitUsage)
	}

	if data, err := os.ReadFile(logPath); err == nil && strings.TrimSpace(string(data)) != "" {
		t.Fatalf("external amux was invoked unexpectedly:\n%s", string(data))
	}
}

func TestCmdAssistantDXContinueHonorsMaxStepsAndTurnBudget(t *testing.T) {
	fakeStepDir := t.TempDir()
	fakeStepPath := filepath.Join(fakeStepDir, "fake-step.sh")
	counterPath := filepath.Join(fakeStepDir, "counter.txt")
	if err := os.WriteFile(fakeStepPath, []byte(`#!/usr/bin/env bash
set -euo pipefail
count_file="${FAKE_STEP_COUNT_FILE:?missing FAKE_STEP_COUNT_FILE}"
count=0
if [[ -f "$count_file" ]]; then
  count="$(cat "$count_file")"
fi
count=$((count + 1))
printf '%s' "$count" > "$count_file"
case "$count" in
  1) printf '%s' "${FAKE_STEP_1_JSON:?missing FAKE_STEP_1_JSON}" ;;
  *) printf '%s' "${FAKE_STEP_2_JSON:?missing FAKE_STEP_2_JSON}" ;;
esac
`), 0o755); err != nil {
		t.Fatalf("write fake step script: %v", err)
	}

	t.Setenv("AMUX_ASSISTANT_TURN_STEP_SCRIPT", fakeStepPath)
	t.Setenv("FAKE_STEP_COUNT_FILE", counterPath)
	t.Setenv("FAKE_STEP_1_JSON", `{"ok":true,"mode":"send","status":"timed_out","summary":"Need one more cycle.","agent_id":"agent-1","workspace_id":"ws-1","assistant":"droid","response":{"substantive_output":false,"needs_input":false},"next_action":"Continue once more.","suggested_command":""}`)
	t.Setenv("FAKE_STEP_2_JSON", `{"ok":true,"mode":"send","status":"idle","summary":"Follow-up finished.","agent_id":"agent-1","workspace_id":"ws-1","assistant":"droid","response":{"substantive_output":true,"needs_input":false},"next_action":"Review final status.","suggested_command":""}`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cmdAssistantDX(&stdout, &stderr, GlobalFlags{}, []string{
		"continue",
		"--agent", "agent-1",
		"--assistant", "droid",
		"--text", "Continue and summarize status.",
		"--max-steps", "3",
		"--turn-budget", "120",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdAssistantDX() code = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, ExitOK, stdout.String(), stderr.String())
	}

	var payload assistantDXPayload
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("payload.OK = false, want true: %#v", payload)
	}
	if payload.Status != "ok" {
		t.Fatalf("payload.Status = %q, want ok", payload.Status)
	}

	data, ok := payload.Data.(map[string]any)
	if !ok {
		t.Fatalf("payload.Data type = %T, want map[string]any", payload.Data)
	}
	turn, ok := data["turn"].(map[string]any)
	if !ok {
		t.Fatalf("data.turn type = %T, want map[string]any", data["turn"])
	}
	if got, _ := turn["steps_used"].(float64); got != 2 {
		t.Fatalf("turn.steps_used = %v, want 2", got)
	}
	if got, _ := turn["max_steps"].(float64); got != 3 {
		t.Fatalf("turn.max_steps = %v, want 3", got)
	}
	if got, _ := turn["turn_budget_seconds"].(float64); got != 120 {
		t.Fatalf("turn.turn_budget_seconds = %v, want 120", got)
	}

	rawCount, err := os.ReadFile(counterPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", counterPath, err)
	}
	if strings.TrimSpace(string(rawCount)) != "2" {
		t.Fatalf("step invocation count = %q, want 2", strings.TrimSpace(string(rawCount)))
	}
}

func TestCmdAssistantDXTaskStartDoesNotForwardMaxStepsOrTurnBudget(t *testing.T) {
	callLog := filepath.Join(t.TempDir(), "amux.log")
	fakeAmuxPath := filepath.Join(t.TempDir(), "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
printf '%s' '{"ok":true,"data":{"status":"idle","overall_status":"completed","summary":"Started.","next_action":"Check status.","quick_actions":[]},"error":null}'
`)

	t.Setenv("AMUX_BIN", fakeAmuxPath)
	t.Setenv("AMUX_CALL_LOG", callLog)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cmdAssistantDX(&stdout, &stderr, GlobalFlags{}, []string{
		"task", "start",
		"--workspace", "ws-1",
		"--assistant", "droid",
		"--prompt", "Review current uncommitted changes",
		"--max-steps", "3",
		"--turn-budget", "120",
	}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdAssistantDX() code = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, ExitOK, stdout.String(), stderr.String())
	}

	raw, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", callLog, err)
	}
	logText := strings.TrimSpace(string(raw))
	if !strings.Contains(logText, "--json task start --workspace ws-1 --assistant droid --prompt Review current uncommitted changes") {
		t.Fatalf("amux log = %q, want task start invocation", logText)
	}
	if strings.Contains(logText, "--max-steps") {
		t.Fatalf("amux log = %q, did not expect forwarded --max-steps", logText)
	}
	if strings.Contains(logText, "--turn-budget") {
		t.Fatalf("amux log = %q, did not expect forwarded --turn-budget", logText)
	}
}

func TestCmdAssistantDXReviewDoesNotForwardMaxStepsOrTurnBudget(t *testing.T) {
	callLog := filepath.Join(t.TempDir(), "amux.log")
	fakeAmuxPath := filepath.Join(t.TempDir(), "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
printf '%s' '{"ok":true,"data":{"status":"idle","overall_status":"completed","summary":"Review done.","next_action":"Ship changes.","quick_actions":[]},"error":null}'
`)

	t.Setenv("AMUX_BIN", fakeAmuxPath)
	t.Setenv("AMUX_CALL_LOG", callLog)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cmdAssistantDX(&stdout, &stderr, GlobalFlags{}, []string{
		"review",
		"--workspace", "ws-9",
		"--assistant", "droid",
		"--max-steps", "2",
		"--turn-budget", "180",
		"--no-monitor",
	}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdAssistantDX() code = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, ExitOK, stdout.String(), stderr.String())
	}

	raw, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", callLog, err)
	}
	logText := strings.TrimSpace(string(raw))
	if !strings.Contains(logText, "--json task start --workspace ws-9 --assistant droid --prompt Review current uncommitted changes") {
		t.Fatalf("amux log = %q, want review to invoke task start", logText)
	}
	if strings.Contains(logText, "--max-steps") {
		t.Fatalf("amux log = %q, did not expect forwarded --max-steps", logText)
	}
	if strings.Contains(logText, "--turn-budget") {
		t.Fatalf("amux log = %q, did not expect forwarded --turn-budget", logText)
	}
}

func TestCmdAssistantDXGitShipFallsBackWhenWorkspaceListAllUnsupported(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoRoot := t.TempDir()
	runGit(t, repoRoot, "init")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "amux-test")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("before\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	runGit(t, repoRoot, "add", "README.md")
	runGit(t, repoRoot, "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("after\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	callLog := filepath.Join(t.TempDir(), "amux.log")
	fakeAmuxPath := filepath.Join(t.TempDir(), "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "$*" in
  "workspace list --all")
    printf '%s' '{"ok":false,"error":{"code":"unsupported","message":"unknown flag: --all"}}'
    ;;
  "workspace list")
    printf '%s' "${FAKE_WORKSPACE_LIST_JSON:?missing FAKE_WORKSPACE_LIST_JSON}"
    ;;
  "workspace list --archived --all"|"workspace list --archived")
    printf '%s' '{"ok":false,"error":{"code":"unsupported","message":"unknown flag: --archived"}}'
    ;;
  *)
    printf '%s' '{"ok":false,"error":{"code":"unexpected","message":"unexpected args"}}'
    exit 2
    ;;
esac
`)

	t.Setenv("AMUX_BIN", fakeAmuxPath)
	t.Setenv("AMUX_CALL_LOG", callLog)
	t.Setenv("FAKE_WORKSPACE_LIST_JSON", `{"ok":true,"data":[{"id":"ws-1","root":"`+repoRoot+`"}],"error":null}`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cmdAssistantDX(&stdout, &stderr, GlobalFlags{}, []string{"git", "ship", "--workspace", "ws-1"}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdAssistantDX() code = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, ExitOK, stdout.String(), stderr.String())
	}

	var payload assistantDXPayload
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("payload.OK = false, want true: %#v", payload)
	}

	raw, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", callLog, err)
	}
	logText := string(raw)
	if !strings.Contains(logText, "--json workspace list --all") {
		t.Fatalf("amux log = %q, want initial --all probe", logText)
	}
	if !strings.Contains(logText, "--json workspace list\n") && !strings.HasSuffix(logText, "--json workspace list") {
		t.Fatalf("amux log = %q, want fallback workspace list call", logText)
	}

	if got := runGitOutput(t, repoRoot, "rev-list", "--count", "HEAD"); got != "2" {
		t.Fatalf("git rev-list --count HEAD = %q, want 2", got)
	}
}
