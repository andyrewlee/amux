package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAssistantTurn_InvalidStepScriptPathReturnsCommandError(t *testing.T) {
	missingStepPath := filepath.Join(t.TempDir(), "missing-step.sh")
	t.Setenv("AMUX_ASSISTANT_TURN_STEP_SCRIPT", missingStepPath)
	t.Setenv("AMUX_ASSISTANT_TURN_SCRIPT_DIR", "")

	payload, exitCode := runAssistantTurn(assistantTurnOptions{
		Mode:          assistantStepModeRun,
		Workspace:     "ws-1",
		Assistant:     "codex",
		Prompt:        "hello",
		WaitTimeout:   "1s",
		IdleThreshold: "1s",
		MaxSteps:      "1",
		TurnBudget:    "30",
	})
	if exitCode != ExitUsage {
		t.Fatalf("exitCode = %d, want %d", exitCode, ExitUsage)
	}

	errPayload, ok := payload.(assistantTurnErrorPayload)
	if !ok {
		t.Fatalf("payload type = %T, want assistantTurnErrorPayload", payload)
	}
	if errPayload.OK {
		t.Fatalf("payload.OK = %v, want false", errPayload.OK)
	}
	if errPayload.Status != "command_error" {
		t.Fatalf("payload.Status = %q, want %q", errPayload.Status, "command_error")
	}
	if errPayload.Summary != "assistant-step.sh is not executable" {
		t.Fatalf("payload.Summary = %q, want %q", errPayload.Summary, "assistant-step.sh is not executable")
	}
	if !strings.Contains(errPayload.Error, missingStepPath) {
		t.Fatalf("payload.Error = %q, want missing path %q", errPayload.Error, missingStepPath)
	}
}

func TestRunAssistantTurn_SendFallsBackToInternalStepWhenScriptUnavailable(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir(%q) error = %v", tempDir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})

	fakeNativePath := filepath.Join(tempDir, "amux-native")
	logPath := filepath.Join(tempDir, "native.log")
	writeExecutable(t, fakeNativePath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
printf '%s' '{"ok":true,"data":{"session_name":"sess-internal","agent_id":"agent-1","workspace_id":"ws-1","assistant":"codex","response":{"status":"idle","latest_line":"done","summary":"done","delta":"done","needs_input":false,"input_hint":"","timed_out":false,"session_exited":false,"changed":true}}}'
`)

	t.Setenv("AMUX_ASSISTANT_NATIVE_BIN", fakeNativePath)
	t.Setenv("AMUX_ASSISTANT_TURN_STEP_SCRIPT", "")
	t.Setenv("AMUX_ASSISTANT_TURN_SCRIPT_DIR", "")
	t.Setenv("AMUX_CALL_LOG", logPath)

	payload, exitCode := runAssistantTurn(assistantTurnOptions{
		Mode:          assistantStepModeSend,
		AgentID:       "agent-1",
		Text:          "Continue",
		Enter:         true,
		WaitTimeout:   "1s",
		IdleThreshold: "1s",
		MaxSteps:      "1",
		TurnBudget:    "30",
	})
	if exitCode != ExitOK {
		t.Fatalf("exitCode = %d, want %d", exitCode, ExitOK)
	}

	turnPayload, ok := payload.(assistantTurnPayload)
	if !ok {
		t.Fatalf("payload type = %T, want assistantTurnPayload", payload)
	}
	if !turnPayload.OK {
		t.Fatalf("payload.OK = %v, want true", turnPayload.OK)
	}

	rawArgs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read native log: %v", err)
	}
	if !strings.Contains(string(rawArgs), "--json agent send --agent agent-1 --text Continue --wait --wait-timeout 1s --idle-threshold 1s --enter") {
		t.Fatalf("internal step did not invoke native amux with expected args: %q", strings.TrimSpace(string(rawArgs)))
	}
}
