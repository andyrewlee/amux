package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdAssistantDXContinue_TextDoesNotAutoSubmitWithoutEnter(t *testing.T) {
	requireBinary(t, "bash")

	fakeStepDir := t.TempDir()
	fakeStepPath := filepath.Join(fakeStepDir, "fake-step.sh")
	callLog := filepath.Join(fakeStepDir, "calls.log")
	writeExecutable(t, fakeStepPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
printf '%s' '{"ok":true,"mode":"send","status":"idle","summary":"Continue completed.","agent_id":"agent-1","workspace_id":"ws-1","assistant":"droid","response":{"substantive_output":true,"needs_input":false},"next_action":"Run status.","suggested_command":""}'
`)

	t.Setenv("AMUX_ASSISTANT_TURN_STEP_SCRIPT", fakeStepPath)
	t.Setenv("AMUX_CALL_LOG", callLog)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cmdAssistantDX(&stdout, &stderr, GlobalFlags{}, []string{
		"continue",
		"--agent", "agent-1",
		"--assistant", "droid",
		"--text", "Continue and summarize status.",
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
	if payload.Status != "ok" {
		t.Fatalf("status = %q, want ok", payload.Status)
	}

	raw, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	logText := string(raw)
	if !strings.Contains(logText, "send --agent agent-1 --text Continue and summarize status.") {
		t.Fatalf("agent send should include follow-up text, got:\n%s", logText)
	}
	if strings.Contains(logText, "--enter") {
		t.Fatalf("agent send should not auto-append --enter when only --text is provided, got:\n%s", logText)
	}
}

func TestCmdAssistantDXContinue_AllowsWaitOnlyResumeWithoutNewInput(t *testing.T) {
	requireBinary(t, "bash")

	fakeStepDir := t.TempDir()
	fakeStepPath := filepath.Join(fakeStepDir, "fake-step.sh")
	callLog := filepath.Join(fakeStepDir, "calls.log")
	writeExecutable(t, fakeStepPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
printf '%s' '{"ok":true,"mode":"send","status":"idle","summary":"Continue completed.","agent_id":"agent-1","workspace_id":"ws-1","assistant":"droid","response":{"substantive_output":true,"needs_input":false},"next_action":"Run status.","suggested_command":""}'
`)

	t.Setenv("AMUX_ASSISTANT_TURN_STEP_SCRIPT", fakeStepPath)
	t.Setenv("AMUX_CALL_LOG", callLog)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cmdAssistantDX(&stdout, &stderr, GlobalFlags{}, []string{
		"continue",
		"--agent", "agent-1",
		"--assistant", "droid",
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
	if payload.Status != "ok" {
		t.Fatalf("status = %q, want ok", payload.Status)
	}

	raw, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	logText := string(raw)
	if !strings.Contains(logText, "send --agent agent-1") {
		t.Fatalf("agent send should still be invoked for wait-only continue, got:\n%s", logText)
	}
	if strings.Contains(logText, "--text") {
		t.Fatalf("wait-only continue should not send new text, got:\n%s", logText)
	}
	if strings.Contains(logText, "--enter") {
		t.Fatalf("wait-only continue should not auto-submit enter, got:\n%s", logText)
	}
}
