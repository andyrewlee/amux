package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistantStepScriptRun_NeedsInputChoicePromptsAddReplyActions(t *testing.T) {
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

	runJSON := `{"ok":true,"data":{"session_name":"sess-choices","agent_id":"agent-choices","workspace_id":"ws-choices","assistant":"codex","response":{"status":"needs_input","latest_line":"Pick one","summary":"Pick one:\n1. Continue with codex\n2. Continue with claude\n3. Cancel\nA. Use project workspace\nB. Use nested workspace\nReply yes or no\nPress Enter to continue","delta":"Pick one:\n1. Continue with codex\n2. Continue with claude\n3. Cancel\nA. Use project workspace\nB. Use nested workspace\nReply yes or no\nPress Enter to continue","needs_input":true,"input_hint":"Pick one:\n1. Continue with codex\n2. Continue with claude\n3. Cancel\nA. Use project workspace\nB. Use nested workspace\nReply yes or no\nPress Enter to continue","timed_out":false,"session_exited":false,"changed":true}}}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-choices",
		"--assistant", "codex",
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

	if got, _ := payload["status"].(string); got != "needs_input" {
		t.Fatalf("status = %q, want %q", got, "needs_input")
	}
	nextAction, _ := payload["next_action"].(string)
	if !strings.Contains(nextAction, "choose one of the listed options") {
		t.Fatalf("next_action = %q, want explicit choice guidance", nextAction)
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	wantIDs := map[string]bool{
		"reply_1":     false,
		"reply_2":     false,
		"reply_3":     false,
		"reply_a":     false,
		"reply_b":     false,
		"reply_yes":   false,
		"reply_no":    false,
		"reply_enter": false,
	}
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		if _, exists := wantIDs[id]; exists {
			wantIDs[id] = true
		}
	}
	for id, seen := range wantIDs {
		if !seen {
			t.Fatalf("missing quick action %s in %#v", id, quickActions)
		}
	}
}

func TestAssistantStepScriptRun_NeedsInputIncludesExtendedChoiceActions(t *testing.T) {
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

	runJSON := `{"ok":true,"data":{"session_name":"sess-choices","agent_id":"agent-choices","workspace_id":"ws-choices","assistant":"codex","response":{"status":"needs_input","latest_line":"Pick one","summary":"Pick one:\n1. Option one\n2. Option two\n3. Option three\n4. Option four\n5. Option five\nA. Option A\nB. Option B\nC. Option C\nD. Option D\nE. Option E","delta":"Pick one:\n1. Option one\n2. Option two\n3. Option three\n4. Option four\n5. Option five\nA. Option A\nB. Option B\nC. Option C\nD. Option D\nE. Option E","needs_input":true,"input_hint":"Pick one:\n1. Option one\n2. Option two\n3. Option three\n4. Option four\n5. Option five\nA. Option A\nB. Option B\nC. Option C\nD. Option D\nE. Option E","timed_out":false,"session_exited":false,"changed":true}}}`

	cmd := exec.Command(
		scriptPath,
		"run",
		"--workspace", "ws-choices",
		"--assistant", "codex",
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

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	wantIDs := map[string]bool{
		"reply_4": false,
		"reply_5": false,
		"reply_d": false,
		"reply_e": false,
	}
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		if _, exists := wantIDs[id]; exists {
			wantIDs[id] = true
		}
	}
	for id, seen := range wantIDs {
		if !seen {
			t.Fatalf("missing quick action %s in %#v", id, quickActions)
		}
	}
}
