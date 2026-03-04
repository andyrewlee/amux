package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistantStepScriptRun_StripsDroidChromeFromSummary(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-step.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	if err := os.WriteFile(fakeAmuxPath, []byte(`#!/usr/bin/env bash
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
`), 0o755); err != nil {
		t.Fatalf("write fake amux: %v", err)
	}

	runJSON := `{"ok":true,"data":{"session_name":"sess-droid","agent_id":"agent-droid","workspace_id":"ws-droid","assistant":"droid","response":{"status":"idle","latest_line":"autonomy                                                models","summary":"Completed in 1 step(s). autonomy                                                models","delta":"v0.60.0\nYou are standing in an open terminal. An AI awaits your commands.\nENTER to send • \\ + ENTER for a new line • @ to mention files\nCurrent folder: /tmp/repo\n> Reply exactly READY in one line.\n⛬ READY\nAuto (High) - allow all commands             GLM-5 [Z.AI Coding Plan] [custom]\nshift+tab to cycle modes (auto/spec), ctrl+L for        ctrl+N to cycle\nautonomy                                                models\n[⏱ 1m 18s]? for help                                                     IDE ◌","needs_input":false,"input_hint":"","timed_out":false,"session_exited":false,"changed":true}}}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-droid",
		"--assistant", "droid",
		"--prompt", "test prompt",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "FAKE_AMUX_RUN_JSON", runJSON)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("assistant-step.sh run failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}

	summary, _ := payload["summary"].(string)
	if strings.Contains(strings.ToLower(summary), "autonomy") || strings.Contains(summary, "Current folder:") || strings.Contains(summary, "You are standing in an open terminal.") {
		t.Fatalf("summary leaked droid chrome: %q", summary)
	}
	if !strings.Contains(summary, "READY") {
		t.Fatalf("summary = %q, expected retained agent content", summary)
	}
	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	msg, _ := channel["message"].(string)
	if strings.Contains(strings.ToLower(msg), "autonomy") || strings.Contains(msg, "Current folder:") || strings.Contains(msg, "You are standing in an open terminal.") {
		t.Fatalf("channel.message leaked droid chrome: %q", msg)
	}
}

func TestAssistantStepScriptRun_PrefersNonPromptSummaryWhenQuotedLineFollowsContext(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-step.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	if err := os.WriteFile(fakeAmuxPath, []byte(`#!/usr/bin/env bash
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
`), 0o755); err != nil {
		t.Fatalf("write fake amux: %v", err)
	}

	runJSON := `{"ok":true,"data":{"session_name":"sess-quoted","agent_id":"agent-quoted","workspace_id":"ws-quoted","assistant":"droid","response":{"status":"idle","latest_line":"Current folder: /tmp/repo","summary":"Current folder: /tmp/repo","delta":"v0.60.0\nCurrent folder: /tmp/repo\n> Reply exactly READY in one line.\nPlan recap:\n> The user asked to fix the login bug","needs_input":false,"input_hint":"","timed_out":false,"session_exited":false,"changed":true}}}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-quoted",
		"--assistant", "droid",
		"--prompt", "test prompt",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "FAKE_AMUX_RUN_JSON", runJSON)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("assistant-step.sh run failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}

	summary, _ := payload["summary"].(string)
	want := "Plan recap:"
	if summary != want {
		t.Fatalf("summary = %q, want %q", summary, want)
	}
}

func TestAssistantStepScriptRun_TimeoutIgnoresStandalonePromptEcho(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-step.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	if err := os.WriteFile(fakeAmuxPath, []byte(`#!/usr/bin/env bash
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
`), 0o755); err != nil {
		t.Fatalf("write fake amux: %v", err)
	}

	runJSON := `{"ok":true,"data":{"session_name":"sess-timeout","agent_id":"agent-timeout","workspace_id":"ws-timeout","assistant":"droid","response":{"status":"timed_out","latest_line":"(no output yet)","summary":"(no output yet)","delta":"v0.60.0\nCurrent folder: /tmp/repo\n> Please fix login bug","needs_input":false,"input_hint":"","timed_out":true,"session_exited":false,"changed":false}}}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-timeout",
		"--assistant", "droid",
		"--prompt", "test prompt",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "FAKE_AMUX_RUN_JSON", runJSON)
	env = withEnv(env, "AMUX_ASSISTANT_STEP_TIMEOUT_RECOVERY_POLLS", "0")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("assistant-step.sh run failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}

	summary, _ := payload["summary"].(string)
	want := "Timed out waiting for first visible output; agent may still be starting."
	if summary != want {
		t.Fatalf("summary = %q, want %q", summary, want)
	}
}

func TestAssistantStepScriptRun_TimeoutIgnoresPromptEchoInLatestLineAndSummary(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-step.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	if err := os.WriteFile(fakeAmuxPath, []byte(`#!/usr/bin/env bash
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
`), 0o755); err != nil {
		t.Fatalf("write fake amux: %v", err)
	}

	runJSON := `{"ok":true,"data":{"session_name":"sess-timeout2","agent_id":"agent-timeout2","workspace_id":"ws-timeout2","assistant":"droid","response":{"status":"timed_out","latest_line":"> Please fix login bug","summary":"> Please fix login bug","delta":"", "needs_input":false,"input_hint":"","timed_out":true,"session_exited":false,"changed":false}}}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-timeout2",
		"--assistant", "droid",
		"--prompt", "test prompt",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "FAKE_AMUX_RUN_JSON", runJSON)
	env = withEnv(env, "AMUX_ASSISTANT_STEP_TIMEOUT_RECOVERY_POLLS", "0")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("assistant-step.sh run failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}

	summary, _ := payload["summary"].(string)
	want := "Timed out waiting for first visible output; agent may still be starting."
	if summary != want {
		t.Fatalf("summary = %q, want %q", summary, want)
	}
}
