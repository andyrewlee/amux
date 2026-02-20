package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestCaptureAgentPaneWithRetry_EventualSuccess(t *testing.T) {
	origCapture := tmuxCapturePaneTail
	origAttempts := agentCaptureRetryAttempts
	origDelay := agentCaptureRetryDelay
	defer func() {
		tmuxCapturePaneTail = origCapture
		agentCaptureRetryAttempts = origAttempts
		agentCaptureRetryDelay = origDelay
	}()

	agentCaptureRetryAttempts = 4
	agentCaptureRetryDelay = 0

	calls := 0
	tmuxCapturePaneTail = func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		if calls < 3 {
			return "", false
		}
		return "captured content", true
	}

	content, ok := captureAgentPaneWithRetry("session", 40, tmux.Options{})
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if content != "captured content" {
		t.Fatalf("content = %q, want %q", content, "captured content")
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want %d", calls, 3)
	}
}

func TestCaptureAgentPaneWithRetry_AllAttemptsFail(t *testing.T) {
	origCapture := tmuxCapturePaneTail
	origAttempts := agentCaptureRetryAttempts
	origDelay := agentCaptureRetryDelay
	defer func() {
		tmuxCapturePaneTail = origCapture
		agentCaptureRetryAttempts = origAttempts
		agentCaptureRetryDelay = origDelay
	}()

	agentCaptureRetryAttempts = 3
	agentCaptureRetryDelay = 1 * time.Millisecond

	calls := 0
	tmuxCapturePaneTail = func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		return "", false
	}

	content, ok := captureAgentPaneWithRetry("session", 40, tmux.Options{})
	if ok {
		t.Fatalf("ok = true, want false")
	}
	if content != "" {
		t.Fatalf("content = %q, want empty", content)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want %d", calls, 3)
	}
}

func TestCmdAgentCaptureJSON_ReturnsSessionExitedWhenMissing(t *testing.T) {
	origCapture := tmuxCapturePaneTail
	origState := tmuxSessionStateFor
	origAttempts := agentCaptureRetryAttempts
	origDelay := agentCaptureRetryDelay
	defer func() {
		tmuxCapturePaneTail = origCapture
		tmuxSessionStateFor = origState
		agentCaptureRetryAttempts = origAttempts
		agentCaptureRetryDelay = origDelay
	}()

	agentCaptureRetryAttempts = 1
	agentCaptureRetryDelay = 0
	tmuxCapturePaneTail = func(_ string, _ int, _ tmux.Options) (string, bool) {
		return "", false
	}
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: false, HasLivePane: false}, nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdAgentCapture(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"session-x", "--lines", "40"},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("code = %d, want %d", code, ExitOK)
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got error=%#v", env.Error)
	}
	payload, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", env.Data)
	}
	if got, _ := payload["status"].(string); got != "session_exited" {
		t.Fatalf("status = %q, want %q", got, "session_exited")
	}
	if got, _ := payload["summary"].(string); got != "Agent session exited before capture." {
		t.Fatalf("summary = %q", got)
	}
	if got, _ := payload["session_exited"].(bool); !got {
		t.Fatalf("session_exited = %v, want true", got)
	}
	if got, _ := payload["content"].(string); got != "" {
		t.Fatalf("content = %q, want empty", got)
	}
}

func TestCmdAgentCaptureJSON_ReturnsCaptureFailedWhenSessionStillExists(t *testing.T) {
	origCapture := tmuxCapturePaneTail
	origState := tmuxSessionStateFor
	origAttempts := agentCaptureRetryAttempts
	origDelay := agentCaptureRetryDelay
	defer func() {
		tmuxCapturePaneTail = origCapture
		tmuxSessionStateFor = origState
		agentCaptureRetryAttempts = origAttempts
		agentCaptureRetryDelay = origDelay
	}()

	agentCaptureRetryAttempts = 1
	agentCaptureRetryDelay = 0
	tmuxCapturePaneTail = func(_ string, _ int, _ tmux.Options) (string, bool) {
		return "", false
	}
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdAgentCapture(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"session-y", "--lines", "40"},
		"test-v1",
	)
	if code != ExitNotFound {
		t.Fatalf("code = %d, want %d", code, ExitNotFound)
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode envelope: %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "capture_failed" {
		t.Fatalf("error code = %#v, want capture_failed", env.Error)
	}
}

func TestCmdAgentCaptureJSON_FallsThroughWhenStateCheckFails(t *testing.T) {
	origCapture := tmuxCapturePaneTail
	origState := tmuxSessionStateFor
	origAttempts := agentCaptureRetryAttempts
	origDelay := agentCaptureRetryDelay
	defer func() {
		tmuxCapturePaneTail = origCapture
		tmuxSessionStateFor = origState
		agentCaptureRetryAttempts = origAttempts
		agentCaptureRetryDelay = origDelay
	}()

	agentCaptureRetryAttempts = 1
	agentCaptureRetryDelay = 0
	tmuxCapturePaneTail = func(_ string, _ int, _ tmux.Options) (string, bool) {
		return "", false
	}
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{}, errors.New("tmux not available")
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdAgentCapture(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"session-err", "--lines", "40"},
		"test-v1",
	)
	if code != ExitNotFound {
		t.Fatalf("code = %d, want %d", code, ExitNotFound)
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode envelope: %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "capture_failed" {
		t.Fatalf("error code = %#v, want capture_failed", env.Error)
	}
}

func TestCmdAgentCaptureJSON_IncludesSignalsOnSuccess(t *testing.T) {
	origCapture := tmuxCapturePaneTail
	origAttempts := agentCaptureRetryAttempts
	origDelay := agentCaptureRetryDelay
	defer func() {
		tmuxCapturePaneTail = origCapture
		agentCaptureRetryAttempts = origAttempts
		agentCaptureRetryDelay = origDelay
	}()

	agentCaptureRetryAttempts = 1
	agentCaptureRetryDelay = 0
	tmuxCapturePaneTail = func(_ string, _ int, _ tmux.Options) (string, bool) {
		return "Working (2s • esc to interrupt)\nDo you want me to proceed? (y/N)\n", true
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdAgentCapture(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"session-z", "--lines", "40"},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("code = %d, want %d", code, ExitOK)
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got error=%#v", env.Error)
	}
	payload, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", env.Data)
	}
	if got, _ := payload["status"].(string); got != "captured" {
		t.Fatalf("status = %q, want %q", got, "captured")
	}
	if got, _ := payload["latest_line"].(string); got != "Do you want me to proceed? (y/N)" {
		t.Fatalf("latest_line = %q", got)
	}
	if got, _ := payload["summary"].(string); got != "Needs input: Do you want me to proceed? (y/N)" {
		t.Fatalf("summary = %q", got)
	}
	if got, _ := payload["needs_input"].(bool); !got {
		t.Fatalf("needs_input = %v, want true", got)
	}
}
