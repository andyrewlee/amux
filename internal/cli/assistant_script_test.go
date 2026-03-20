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

func copyScriptExecutable(t *testing.T, src, dst string) {
	t.Helper()
	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	writeExecutable(t, dst, string(body))
}

// copyScriptWithLib copies a script and its lib/wrapper.sh dependency into the
// destination directory, preserving the relative lib/ layout so that
// `source "$SCRIPT_DIR/lib/wrapper.sh"` works when the script is run from a
// temp dir outside the checkout.
func copyScriptWithLib(t *testing.T, srcScript, dstScript string) {
	t.Helper()
	copyScriptExecutable(t, srcScript, dstScript)

	srcDir := filepath.Dir(srcScript)
	dstDir := filepath.Dir(dstScript)
	wrapperSrc := filepath.Join(srcDir, "lib", "wrapper.sh")
	wrapperDst := filepath.Join(dstDir, "lib", "wrapper.sh")
	if err := os.MkdirAll(filepath.Join(dstDir, "lib"), 0o755); err != nil {
		t.Fatalf("mkdir lib: %v", err)
	}
	copyScriptExecutable(t, wrapperSrc, wrapperDst)
}

func runScriptJSONWithInput(t *testing.T, scriptPath string, env []string, input string, args ...string) map[string]any {
	t.Helper()
	cmd := exec.Command(scriptPath, args...)
	cmd.Env = env
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.Bytes()
	if err != nil && len(out) == 0 {
		t.Fatalf("%s %v failed: %v\nstdout:\n%s\nstderr:\n%s", scriptPath, args, err, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}
	return payload
}

func runScriptOutput(t *testing.T, scriptPath string, env []string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(scriptPath, args...)
	cmd.Env = env
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v failed: %v\nstdout:\n%s\nstderr:\n%s", scriptPath, args, err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

func runScriptOutputWithInput(t *testing.T, scriptPath string, env []string, input string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(scriptPath, args...)
	cmd.Env = env
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v failed: %v\nstdout:\n%s\nstderr:\n%s", scriptPath, args, err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

func TestAssistantPresentScript_AugmentsChannelEnvelope(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-present.sh")
	env := withEnv(os.Environ(), "AMUX_ASSISTANT_CHANNEL", "msteams")
	input := `{"ok":true,"summary":"ok","message":"Build complete","quick_actions":[{"id":"status","label":"Status","command":"amux --json status","style":"primary","prompt":"Check status"}],"channel":{"message":"Build complete","chunks_meta":[{"index":1,"total":1,"text":"Build complete"}]}}`

	payload := runScriptJSONWithInput(t, scriptPath, env, input)

	assistant, ok := payload["assistant_ux"].(map[string]any)
	if !ok {
		t.Fatalf("assistant missing or wrong type: %T", payload["assistant_ux"])
	}
	if got, _ := assistant["selected_channel"].(string); got != "msteams" {
		t.Fatalf("assistant_ux.selected_channel = %q, want msteams", got)
	}
	presentation, ok := assistant["presentation"].(map[string]any)
	if !ok {
		t.Fatalf("assistant_ux.presentation missing or wrong type: %T", assistant["presentation"])
	}
	suggestedActions, ok := presentation["suggested_actions"].([]any)
	if !ok || len(suggestedActions) != 1 {
		t.Fatalf("assistant_ux.presentation.suggested_actions = %#v, want len=1", presentation["suggested_actions"])
	}

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) != 1 {
		t.Fatalf("quick_actions = %#v, want len=1", payload["quick_actions"])
	}
	firstAction, ok := quickActions[0].(map[string]any)
	if !ok {
		t.Fatalf("quick_actions[0] wrong type: %T", quickActions[0])
	}
	if got, _ := firstAction["action_id"].(string); got != "status" {
		t.Fatalf("quick_actions[0].action_id = %q, want status", got)
	}

	actionMap, ok := payload["quick_action_by_id"].(map[string]any)
	if !ok {
		t.Fatalf("quick_action_by_id missing or wrong type: %T", payload["quick_action_by_id"])
	}
	if got, _ := actionMap["status"].(string); got != "amux --json status" {
		t.Fatalf("quick_action_by_id[status] = %q, want %q", got, "amux --json status")
	}
}

func TestAssistantPresentWrapper_DoesNotTreatAMUXBinAsExplicitOverride(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-present.sh")
	fakeBinDir := t.TempDir()
	fakeGoPath := filepath.Join(fakeBinDir, "go")
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	goLogPath := filepath.Join(fakeBinDir, "go-call.log")
	amuxLogPath := filepath.Join(fakeBinDir, "amux-call.log")
	writeExecutable(t, fakeGoPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${GO_CALL_LOG:?missing GO_CALL_LOG}"
cat
`)
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "$*" > "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
echo "Unknown command: assistant" >&2
exit 2
`)

	env := withEnv(os.Environ(), "PATH", fakeBinDir+":/usr/bin:/bin")
	env = withEnv(env, "AMUX_BIN", fakeAmuxPath)
	env = withEnv(env, "GO_CALL_LOG", goLogPath)
	env = withEnv(env, "AMUX_CALL_LOG", amuxLogPath)

	payload := runScriptJSONWithInput(t, scriptPath, env, `{"ok":true,"summary":"ok","channel":{"message":"Build complete","chunks_meta":[{"index":1,"total":1,"text":"Build complete"}]}}`)
	if got, _ := payload["summary"].(string); got != "ok" {
		t.Fatalf("summary = %q, want ok", got)
	}

	rawGo, err := os.ReadFile(goLogPath)
	if err != nil {
		t.Fatalf("read GO_CALL_LOG: %v", err)
	}
	if !strings.Contains(string(rawGo), "assistant present") {
		t.Fatalf("go run was not called with expected args: %q", strings.TrimSpace(string(rawGo)))
	}
	if rawAmux, err := os.ReadFile(amuxLogPath); err == nil && strings.TrimSpace(string(rawAmux)) != "" {
		t.Fatalf("assistant-present should not have treated AMUX_BIN as an explicit override: %q", strings.TrimSpace(string(rawAmux)))
	}
}

func TestAssistantStepWrapper_UsesChannelAndWrapperSuggestions(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-step.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
if [[ "${1:-}" == "agent" && "${2:-}" == "run" ]]; then
  printf '%s' "${FAKE_AMUX_RUN_JSON:?missing FAKE_AMUX_RUN_JSON}"
  exit 0
fi
echo "unexpected args: $*" >&2
exit 2
`)

	runJSON := `{"ok":true,"data":{"session_name":"sess-wrap-1","agent_id":"agent-wrap-1","workspace_id":"ws-wrap-1","assistant":"codex","response":{"status":"timed_out","latest_line":"Still running build","summary":"Timed out; build still running.","delta":"Still running build","needs_input":false,"input_hint":"","timed_out":true,"session_exited":false,"changed":true}}}`
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "FAKE_AMUX_RUN_JSON", runJSON)
	env = withEnv(env, "AMUX_ASSISTANT_CHANNEL", "slack")

	payload := runScriptJSON(t, scriptPath, env,
		"run",
		"--workspace", "ws-wrap-1",
		"--assistant", "codex",
		"--prompt", "continue",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)

	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "assistant-step.sh send --agent") {
		t.Fatalf("suggested_command = %q, expected assistant-step wrapper command", suggested)
	}

	assistant, ok := payload["assistant_ux"].(map[string]any)
	if !ok {
		t.Fatalf("assistant missing or wrong type: %T", payload["assistant_ux"])
	}
	if got, _ := assistant["selected_channel"].(string); got != "slack" {
		t.Fatalf("assistant_ux.selected_channel = %q, want slack", got)
	}
}

func TestAssistantStepWrapper_UsesFallbackAMUXPathOutsideCheckout(t *testing.T) {
	requireBinary(t, "bash")

	originalScriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-step.sh")
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "assistant-step.sh")
	copyScriptWithLib(t, originalScriptPath, scriptPath)

	fakeAmuxPath := filepath.Join(tempDir, "amux-fallback")
	logPath := filepath.Join(tempDir, "amux-call.log")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "$*" > "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
printf '%s' '{"ok":true,"mode":"run","status":"idle","summary":"fallback step ok"}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", "/usr/bin:/bin")
	env = withEnv(env, "AMUX_BIN", "")
	env = withEnv(env, "AMUX_BIN_FALLBACKS", fakeAmuxPath)
	env = withEnv(env, "AMUX_CALL_LOG", logPath)
	env = withEnv(env, "AMUX_ASSISTANT_STEP_SKIP_PRESENT", "true")

	payload := runScriptJSONInDir(t, scriptPath, tempDir, env,
		"run",
		"--workspace", "ws-fallback",
		"--assistant", "codex",
		"--prompt", "hello",
	)

	if got, _ := payload["summary"].(string); got != "fallback step ok" {
		t.Fatalf("summary = %q, want %q", got, "fallback step ok")
	}

	rawArgs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read AMUX_CALL_LOG: %v", err)
	}
	if !strings.Contains(string(rawArgs), "assistant step run --workspace ws-fallback --assistant codex --prompt hello") {
		t.Fatalf("fallback amux was not called with expected args: %q", strings.TrimSpace(string(rawArgs)))
	}
}

func TestAssistantStepWrapper_FallsBackToAMUXBinWhenGoRunFails(t *testing.T) {
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-step.sh")
	fakeBinDir := t.TempDir()
	fakeGoPath := filepath.Join(fakeBinDir, "go")
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	logPath := filepath.Join(fakeBinDir, "amux-call.log")
	writeExecutable(t, fakeGoPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' 'go: go.mod file not found in current directory or any parent directory; see '\''go help modules'\''' >&2
exit 1
`)
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "$*" > "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
if [[ "${1:-}" == "assistant" && "${2:-}" == "step" && "${3:-}" == "run" ]]; then
  printf '%s' '{"ok":true,"mode":"run","status":"idle","summary":"fallback step ok"}'
  exit 0
fi
printf '%s' '{"ok":false,"status":"command_error","summary":"unexpected args"}'
exit 2
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":/usr/bin:/bin")
	env = withEnv(env, "AMUX_BIN", fakeAmuxPath)
	env = withEnv(env, "AMUX_CALL_LOG", logPath)
	env = withEnv(env, "AMUX_ASSISTANT_STEP_SKIP_PRESENT", "true")

	payload := runScriptJSON(t, scriptPath, env,
		"run",
		"--workspace", "ws-step-fallback",
		"--assistant", "codex",
		"--prompt", "hello",
	)
	if got, _ := payload["summary"].(string); got != "fallback step ok" {
		t.Fatalf("summary = %q, want %q", got, "fallback step ok")
	}

	rawArgs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read AMUX_CALL_LOG: %v", err)
	}
	if !strings.Contains(string(rawArgs), "assistant step run --workspace ws-step-fallback --assistant codex --prompt hello") {
		t.Fatalf("fallback amux was not called with expected args: %q", strings.TrimSpace(string(rawArgs)))
	}
}

func TestAssistantStepWrapper_PrefersCheckoutGoRunOverExplicitAMUXBin(t *testing.T) {
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-step.sh")
	fakeBinDir := t.TempDir()
	fakeGoPath := filepath.Join(fakeBinDir, "go")
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux-explicit")
	goLogPath := filepath.Join(fakeBinDir, "go-call.log")
	amuxLogPath := filepath.Join(fakeBinDir, "amux-call.log")
	writeExecutable(t, fakeGoPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${GO_CALL_LOG:?missing GO_CALL_LOG}"
printf '%s' '{"ok":true,"mode":"run","status":"idle","summary":"checkout go run ok"}'
`)
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "$*" > "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
printf '%s' '{"ok":false,"status":"command_error","summary":"explicit AMUX_BIN should not be used before checkout"}'
exit 99
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":/usr/bin:/bin")
	env = withEnv(env, "AMUX_BIN", fakeAmuxPath)
	env = withEnv(env, "AMUX_CALL_LOG", amuxLogPath)
	env = withEnv(env, "GO_CALL_LOG", goLogPath)
	env = withEnv(env, "AMUX_ASSISTANT_STEP_SKIP_PRESENT", "true")

	payload := runScriptJSON(t, scriptPath, env,
		"run",
		"--workspace", "ws-explicit",
		"--assistant", "codex",
		"--prompt", "hello",
	)
	if got, _ := payload["summary"].(string); got != "checkout go run ok" {
		t.Fatalf("summary = %q, want %q", got, "checkout go run ok")
	}

	rawGo, err := os.ReadFile(goLogPath)
	if err != nil {
		t.Fatalf("read GO_CALL_LOG: %v", err)
	}
	if !strings.Contains(string(rawGo), "run ./cmd/amux --cwd") || !strings.Contains(string(rawGo), "assistant step run --workspace ws-explicit --assistant codex --prompt hello") {
		t.Fatalf("go run was not called with expected args: %q", strings.TrimSpace(string(rawGo)))
	}
	if rawAmux, err := os.ReadFile(amuxLogPath); err == nil && strings.TrimSpace(string(rawAmux)) != "" {
		t.Fatalf("explicit AMUX_BIN should not have been called before checkout: %q", strings.TrimSpace(string(rawAmux)))
	}
}

func TestAssistantStepWrapper_PrefersCheckoutGoRunOverPATHAmux(t *testing.T) {
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-step.sh")
	fakeBinDir := t.TempDir()
	fakeGoPath := filepath.Join(fakeBinDir, "go")
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	goLogPath := filepath.Join(fakeBinDir, "go-call.log")
	amuxLogPath := filepath.Join(fakeBinDir, "amux-call.log")
	writeExecutable(t, fakeGoPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${GO_CALL_LOG:?missing GO_CALL_LOG}"
printf '%s' '{"ok":true,"mode":"run","status":"idle","summary":"checkout go run ok"}'
`)
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "$*" > "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
printf '%s' '{"ok":false,"status":"command_error","summary":"PATH amux should not have been used"}'
exit 99
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":/usr/bin:/bin")
	env = withEnv(env, "AMUX_CALL_LOG", amuxLogPath)
	env = withEnv(env, "GO_CALL_LOG", goLogPath)
	env = withEnv(env, "AMUX_ASSISTANT_STEP_SKIP_PRESENT", "true")

	payload := runScriptJSON(t, scriptPath, env,
		"run",
		"--workspace", "ws-checkout",
		"--assistant", "codex",
		"--prompt", "hello",
	)
	if got, _ := payload["summary"].(string); got != "checkout go run ok" {
		t.Fatalf("summary = %q, want %q", got, "checkout go run ok")
	}

	rawGo, err := os.ReadFile(goLogPath)
	if err != nil {
		t.Fatalf("read GO_CALL_LOG: %v", err)
	}
	if !strings.Contains(string(rawGo), "run ./cmd/amux --cwd") || !strings.Contains(string(rawGo), "assistant step run --workspace ws-checkout --assistant codex --prompt hello") {
		t.Fatalf("go run was not called with expected args: %q", strings.TrimSpace(string(rawGo)))
	}
	if rawAmux, err := os.ReadFile(amuxLogPath); err == nil && strings.TrimSpace(string(rawAmux)) != "" {
		t.Fatalf("PATH amux should not have been called in checkout mode: %q", strings.TrimSpace(string(rawAmux)))
	}
}

func TestAssistantDXWrapper_UsesChannelAndWrapperSuggestions(t *testing.T) {
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
case "${1:-} ${2:-}" in
  "project list")
    printf '%s' '{"ok":true,"data":[{"name":"api","path":"/tmp/api"},{"name":"mobile","path":"/tmp/mobile"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "AMUX_ASSISTANT_CHANNEL", "discord")

	payload := runScriptJSON(t, scriptPath, env,
		"project", "list",
		"--query", "api",
	)

	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "assistant-dx.sh") {
		t.Fatalf("suggested_command = %q, expected assistant-dx wrapper command", suggested)
	}

	assistant, ok := payload["assistant_ux"].(map[string]any)
	if !ok {
		t.Fatalf("assistant missing or wrong type: %T", payload["assistant_ux"])
	}
	if got, _ := assistant["selected_channel"].(string); got != "discord" {
		t.Fatalf("assistant_ux.selected_channel = %q, want discord", got)
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	firstAction, ok := quickActions[0].(map[string]any)
	if !ok {
		t.Fatalf("quick_actions[0] wrong type: %T", quickActions[0])
	}
	if got, _ := firstAction["action_id"].(string); got == "" {
		t.Fatalf("quick_actions[0].action_id is empty")
	}
}

func TestAssistantDXWrapper_FallsBackToAMUXBinWhenGoRunFails(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeGoPath := filepath.Join(fakeBinDir, "go")
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	logPath := filepath.Join(fakeBinDir, "amux-call.log")
	writeExecutable(t, fakeGoPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' 'go: go.mod file not found in current directory or any parent directory; see '\''go help modules'\''' >&2
exit 1
`)
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "$*" > "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
if [[ "${1:-}" == "assistant" && "${2:-}" == "dx" && "${3:-}" == "project" && "${4:-}" == "list" ]]; then
  printf '%s' '{"ok":true,"command":"project.list","status":"ok","summary":"1 project(s) available.","next_action":"Choose a project.","suggested_command":"assistant-dx.sh project list","data":{"projects":[{"name":"api","path":"/tmp/api"}]},"quick_actions":[],"quick_action_by_id":{},"channel":{"message":"1 project(s) available.","chunks":["1 project(s) available."],"chunks_meta":[{"index":1,"total":1,"text":"1 project(s) available."}],"inline_buttons":[]},"assistant_ux":{"selected_channel":"telegram"}}'
  exit 0
fi
printf '%s' '{"ok":false,"error":{"code":"unexpected","message":"unexpected args"}}'
exit 2
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":/usr/bin:/bin")
	env = withEnv(env, "AMUX_BIN", fakeAmuxPath)
	env = withEnv(env, "AMUX_CALL_LOG", logPath)

	payload := runScriptJSON(t, scriptPath, env, "project", "list")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}

	rawArgs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read AMUX_CALL_LOG: %v", err)
	}
	if !strings.Contains(string(rawArgs), "assistant dx project list") {
		t.Fatalf("fallback amux was not called with expected args: %q", strings.TrimSpace(string(rawArgs)))
	}
}

func TestAssistantDXWrapper_CheckoutModeSetsInternalInvokerEnv(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	body, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read %s: %v", scriptPath, err)
	}
	text := string(body)
	if !strings.Contains(text, "AMUX_ASSISTANT_DX_FORCE_INTERNAL=true") {
		t.Fatalf("assistant-dx.sh should force internal DX invoker in checkout mode")
	}
	if !strings.Contains(text, "AMUX_ASSISTANT_REUSE_SELF_EXEC=true") {
		t.Fatalf("assistant-dx.sh should reuse self executable in checkout mode")
	}
	// The go run invocation now lives in lib/wrapper.sh; verify the script
	// delegates to it with the correct subcommand.
	if !strings.Contains(text, "amux_run_from_checkout") || !strings.Contains(text, "\"assistant dx\"") {
		t.Fatalf("assistant-dx.sh should delegate to amux_run_from_checkout with 'assistant dx' subcommand")
	}
	wrapperPath := filepath.Join("..", "..", "skills", "amux", "scripts", "lib", "wrapper.sh")
	wBody, err := os.ReadFile(wrapperPath)
	if err != nil {
		t.Fatalf("read %s: %v", wrapperPath, err)
	}
	if !strings.Contains(string(wBody), "go run ./cmd/amux --cwd \"$orig_pwd\" $subcmd") {
		t.Fatalf("lib/wrapper.sh should use go run for checkout-native fallback")
	}
}

func TestAssistantDXWrapper_DoesNotFallbackOnCommandFailure(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeGoPath := filepath.Join(fakeBinDir, "go")
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	logPath := filepath.Join(fakeBinDir, "amux-call.log")
	writeExecutable(t, fakeGoPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' '{"ok":false,"command":"project.add","status":"command_error","summary":"project add failed","next_action":"Fix the command and retry.","suggested_command":"","data":{"details":"duplicate project"},"quick_actions":[],"quick_action_by_id":{},"channel":{"message":"project add failed","chunks":["project add failed"],"chunks_meta":[{"index":1,"total":1,"text":"project add failed"}],"inline_buttons":[]},"assistant_ux":{"selected_channel":"telegram"}}'
exit 1
`)
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "$*" > "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
printf '%s' '{"ok":true,"command":"project.add","status":"ok","summary":"unexpected fallback","next_action":"","suggested_command":"","data":{},"quick_actions":[],"quick_action_by_id":{},"channel":{"message":"unexpected fallback","chunks":["unexpected fallback"],"chunks_meta":[{"index":1,"total":1,"text":"unexpected fallback"}],"inline_buttons":[]},"assistant_ux":{"selected_channel":"telegram"}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":/usr/bin:/bin")
	env = withEnv(env, "AMUX_BIN", fakeAmuxPath)
	env = withEnv(env, "AMUX_CALL_LOG", logPath)

	payload := runScriptJSON(t, scriptPath, env, "project", "add", "--path", "/tmp/repo")
	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want command_error", got)
	}
	if got, _ := payload["summary"].(string); got != "project add failed" {
		t.Fatalf("summary = %q, want project add failed", got)
	}
	if rawArgs, err := os.ReadFile(logPath); err == nil && strings.TrimSpace(string(rawArgs)) != "" {
		t.Fatalf("fallback amux should not have been called on command failure: %q", strings.TrimSpace(string(rawArgs)))
	}
}

func TestAssistantDXWrapper_DoesNotFallbackOnGoToolStderrDuringCommandFailure(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeGoPath := filepath.Join(fakeBinDir, "go")
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	logPath := filepath.Join(fakeBinDir, "amux-call.log")
	writeExecutable(t, fakeGoPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' 'go: downloading example.com/fake v1.2.3' >&2
printf '%s' '{"ok":false,"command":"project.add","status":"command_error","summary":"project add failed","next_action":"Fix the command and retry.","suggested_command":"","data":{"details":"duplicate project"},"quick_actions":[],"quick_action_by_id":{},"channel":{"message":"project add failed","chunks":["project add failed"],"chunks_meta":[{"index":1,"total":1,"text":"project add failed"}],"inline_buttons":[]},"assistant_ux":{"selected_channel":"telegram"}}'
exit 1
`)
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "$*" > "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
printf '%s' '{"ok":true,"command":"project.add","status":"ok","summary":"unexpected fallback","next_action":"","suggested_command":"","data":{},"quick_actions":[],"quick_action_by_id":{},"channel":{"message":"unexpected fallback","chunks":["unexpected fallback"],"chunks_meta":[{"index":1,"total":1,"text":"unexpected fallback"}],"inline_buttons":[]},"assistant_ux":{"selected_channel":"telegram"}}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":/usr/bin:/bin")
	env = withEnv(env, "AMUX_BIN", fakeAmuxPath)
	env = withEnv(env, "AMUX_CALL_LOG", logPath)

	payload := runScriptJSON(t, scriptPath, env, "project", "add", "--path", "/tmp/repo")
	if got, _ := payload["status"].(string); got != "command_error" {
		t.Fatalf("status = %q, want command_error", got)
	}
	if rawArgs, err := os.ReadFile(logPath); err == nil && strings.TrimSpace(string(rawArgs)) != "" {
		t.Fatalf("fallback amux should not have been called on go tool stderr noise: %q", strings.TrimSpace(string(rawArgs)))
	}
}

func TestAssistantDXWrapper_UsesAMUXBinOverrideWhenCheckoutUnavailable(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	originalScriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "assistant-dx.sh")
	copyScriptWithLib(t, originalScriptPath, scriptPath)
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux-custom")
	logPath := filepath.Join(fakeBinDir, "amux-call.log")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "$*" > "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project list")
    printf '%s' '{"ok":true,"data":[{"name":"api","path":"/tmp/api"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "AMUX_BIN", fakeAmuxPath)
	env = withEnv(env, "AMUX_CALL_LOG", logPath)
	env = withEnv(env, "PATH", "/usr/bin:/bin")

	payload := runScriptJSON(t, scriptPath, env,
		"project", "list",
		"--query", "api",
	)

	if got, _ := payload["status"].(string); got == "command_error" {
		t.Fatalf("status = %q, expected non-command_error", got)
	}

	rawArgs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read AMUX_CALL_LOG: %v", err)
	}
	if !strings.Contains(string(rawArgs), "project list") {
		t.Fatalf("amux override was not called with expected args: %q", strings.TrimSpace(string(rawArgs)))
	}
}

func TestAssistantDXWrapper_PrefersCheckoutGoRunOverPATHAmux(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeGoPath := filepath.Join(fakeBinDir, "go")
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	goLogPath := filepath.Join(fakeBinDir, "go-call.log")
	amuxLogPath := filepath.Join(fakeBinDir, "amux-call.log")
	writeExecutable(t, fakeGoPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${GO_CALL_LOG:?missing GO_CALL_LOG}"
printf '%s' '{"ok":true,"command":"project.list","status":"ok","summary":"checkout go run ok","next_action":"","suggested_command":"","data":{"projects":[]},"quick_actions":[],"quick_action_by_id":{},"channel":{"message":"checkout go run ok","chunks":["checkout go run ok"],"chunks_meta":[{"index":1,"total":1,"text":"checkout go run ok"}],"inline_buttons":[]},"assistant_ux":{"selected_channel":"telegram"}}'
`)
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "$*" > "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
printf '%s' '{"ok":false,"error":{"code":"unexpected","message":"PATH amux should not have been used"}}'
exit 99
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":/usr/bin:/bin")
	env = withEnv(env, "AMUX_CALL_LOG", amuxLogPath)
	env = withEnv(env, "GO_CALL_LOG", goLogPath)
	env = withEnv(env, "AMUX_BIN", "")

	payload := runScriptJSON(t, scriptPath, env, "project", "list")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}

	rawGo, err := os.ReadFile(goLogPath)
	if err != nil {
		t.Fatalf("read GO_CALL_LOG: %v", err)
	}
	if !strings.Contains(string(rawGo), "run ./cmd/amux --cwd") || !strings.Contains(string(rawGo), "assistant dx project list") {
		t.Fatalf("go run was not called with expected args: %q", strings.TrimSpace(string(rawGo)))
	}
	if rawAmux, err := os.ReadFile(amuxLogPath); err == nil && strings.TrimSpace(string(rawAmux)) != "" {
		t.Fatalf("PATH amux should not have been called in checkout mode: %q", strings.TrimSpace(string(rawAmux)))
	}
}

func TestAssistantDXWrapper_ExplicitAMUXBinDoesNotBypassCheckoutCompatibility(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dx.sh")
	fakeBinDir := t.TempDir()
	fakeGoPath := filepath.Join(fakeBinDir, "go")
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	goLogPath := filepath.Join(fakeBinDir, "go-call.log")
	amuxLogPath := filepath.Join(fakeBinDir, "amux-call.log")
	writeExecutable(t, fakeGoPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${GO_CALL_LOG:?missing GO_CALL_LOG}"
printf '%s' '{"ok":true,"command":"project.list","status":"ok","summary":"checkout go run ok","next_action":"","suggested_command":"","data":{"projects":[]},"quick_actions":[],"quick_action_by_id":{},"channel":{"message":"checkout go run ok","chunks":["checkout go run ok"],"chunks_meta":[{"index":1,"total":1,"text":"checkout go run ok"}],"inline_buttons":[]},"assistant_ux":{"selected_channel":"telegram"}}'
`)
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "$*" > "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
echo "Unknown command: assistant" >&2
exit 2
`)

	env := withEnv(os.Environ(), "PATH", fakeBinDir+":/usr/bin:/bin")
	env = withEnv(env, "AMUX_BIN", fakeAmuxPath)
	env = withEnv(env, "GO_CALL_LOG", goLogPath)
	env = withEnv(env, "AMUX_CALL_LOG", amuxLogPath)

	payload := runScriptJSON(t, scriptPath, env, "project", "list")
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want ok", got)
	}
	rawGo, err := os.ReadFile(goLogPath)
	if err != nil {
		t.Fatalf("read GO_CALL_LOG: %v", err)
	}
	if !strings.Contains(string(rawGo), "assistant dx project list") {
		t.Fatalf("go run was not called with expected args: %q", strings.TrimSpace(string(rawGo)))
	}
	if rawAmux, err := os.ReadFile(amuxLogPath); err == nil && strings.TrimSpace(string(rawAmux)) != "" {
		t.Fatalf("older AMUX_BIN should not have been called before checkout: %q", strings.TrimSpace(string(rawAmux)))
	}
}

func TestAssistantStepWrapper_CheckoutModeSetsReuseSelfExecEnv(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-step.sh")
	body, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read %s: %v", scriptPath, err)
	}
	text := string(body)
	if !strings.Contains(text, "AMUX_ASSISTANT_REUSE_SELF_EXEC=true") {
		t.Fatalf("assistant-step.sh should reuse self executable in checkout mode")
	}
	// The go run invocation now lives in lib/wrapper.sh; verify the script
	// delegates to it with the correct subcommand.
	if !strings.Contains(text, "amux_run_from_checkout") || !strings.Contains(text, "\"assistant step\"") {
		t.Fatalf("assistant-step.sh should delegate to amux_run_from_checkout with 'assistant step' subcommand")
	}
	wrapperPath := filepath.Join("..", "..", "skills", "amux", "scripts", "lib", "wrapper.sh")
	wBody, err := os.ReadFile(wrapperPath)
	if err != nil {
		t.Fatalf("read %s: %v", wrapperPath, err)
	}
	if !strings.Contains(string(wBody), "go run ./cmd/amux --cwd \"$orig_pwd\" $subcmd") {
		t.Fatalf("lib/wrapper.sh should use go run for checkout-native fallback")
	}
}

func TestAssistantTurnWrapper_UsesFallbackAMUXPathOutsideCheckout(t *testing.T) {
	requireBinary(t, "bash")

	originalScriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-turn.sh")
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "assistant-turn.sh")
	copyScriptWithLib(t, originalScriptPath, scriptPath)

	fakeAmuxPath := filepath.Join(tempDir, "amux-fallback")
	logPath := filepath.Join(tempDir, "amux-call.log")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "$*" > "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
printf '%s' '{"ok":true,"mode":"run","status":"idle","overall_status":"completed","summary":"fallback turn ok"}'
`)

	env := os.Environ()
	env = withEnv(env, "PATH", "/usr/bin:/bin")
	env = withEnv(env, "AMUX_BIN", "")
	env = withEnv(env, "AMUX_BIN_FALLBACKS", fakeAmuxPath)
	env = withEnv(env, "AMUX_CALL_LOG", logPath)
	env = withEnv(env, "AMUX_ASSISTANT_TURN_SKIP_PRESENT", "true")

	payload := runScriptJSONInDir(t, scriptPath, tempDir, env,
		"run",
		"--workspace", "ws-turn-fallback",
		"--assistant", "codex",
		"--prompt", "hello",
	)

	if got, _ := payload["summary"].(string); got != "fallback turn ok" {
		t.Fatalf("summary = %q, want %q", got, "fallback turn ok")
	}

	rawArgs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read AMUX_CALL_LOG: %v", err)
	}
	if !strings.Contains(string(rawArgs), "assistant turn run --workspace ws-turn-fallback --assistant codex --prompt hello") {
		t.Fatalf("fallback amux was not called with expected args: %q", strings.TrimSpace(string(rawArgs)))
	}
}

func TestAssistantPollAgentWrapper_UsesNativeBin(t *testing.T) {
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "poll-agent.sh")
	fakeBinDir := t.TempDir()
	fakeNativePath := filepath.Join(fakeBinDir, "amux-native")
	logPath := filepath.Join(fakeBinDir, "native-call.log")
	writeExecutable(t, fakeNativePath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "$*" > "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
printf 'poll native output\n'
`)

	env := withEnv(os.Environ(), "AMUX_ASSISTANT_NATIVE_BIN", fakeNativePath)
	env = withEnv(env, "AMUX_CALL_LOG", logPath)

	stdout, stderr := runScriptOutput(t, scriptPath, env, "--session", "sess-1", "--lines", "25")
	if stdout != "poll native output\n" {
		t.Fatalf("stdout = %q, want %q", stdout, "poll native output\n")
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}

	rawArgs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read AMUX_CALL_LOG: %v", err)
	}
	if !strings.Contains(string(rawArgs), "assistant poll-agent --session sess-1 --lines 25") {
		t.Fatalf("wrapper did not invoke native poll-agent command: %q", strings.TrimSpace(string(rawArgs)))
	}
}

func TestAssistantWaitForIdleWrapper_UsesNativeBin(t *testing.T) {
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "wait-for-idle.sh")
	fakeBinDir := t.TempDir()
	fakeNativePath := filepath.Join(fakeBinDir, "amux-native")
	logPath := filepath.Join(fakeBinDir, "native-call.log")
	writeExecutable(t, fakeNativePath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "$*" > "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
printf 'wait native output\n'
`)

	env := withEnv(os.Environ(), "AMUX_ASSISTANT_NATIVE_BIN", fakeNativePath)
	env = withEnv(env, "AMUX_CALL_LOG", logPath)

	stdout, stderr := runScriptOutput(t, scriptPath, env, "--session", "sess-2", "--timeout", "12")
	if stdout != "wait native output\n" {
		t.Fatalf("stdout = %q, want %q", stdout, "wait native output\n")
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}

	rawArgs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read AMUX_CALL_LOG: %v", err)
	}
	if !strings.Contains(string(rawArgs), "assistant wait-for-idle --session sess-2 --timeout 12") {
		t.Fatalf("wrapper did not invoke native wait-for-idle command: %q", strings.TrimSpace(string(rawArgs)))
	}
}

func TestAssistantFormatCaptureWrapper_UsesNativeBinAndPassesStdin(t *testing.T) {
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "format-capture.sh")
	fakeBinDir := t.TempDir()
	fakeNativePath := filepath.Join(fakeBinDir, "amux-native")
	logPath := filepath.Join(fakeBinDir, "native-call.log")
	writeExecutable(t, fakeNativePath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "$*" > "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
cat
`)

	env := withEnv(os.Environ(), "AMUX_ASSISTANT_NATIVE_BIN", fakeNativePath)
	env = withEnv(env, "AMUX_CALL_LOG", logPath)

	stdout, stderr := runScriptOutputWithInput(t, scriptPath, env, "wrapped input\n", "--trim")
	if stdout != "wrapped input\n" {
		t.Fatalf("stdout = %q, want %q", stdout, "wrapped input\n")
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}

	rawArgs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read AMUX_CALL_LOG: %v", err)
	}
	if !strings.Contains(string(rawArgs), "assistant format-capture --trim") {
		t.Fatalf("wrapper did not invoke native format-capture command: %q", strings.TrimSpace(string(rawArgs)))
	}
}
