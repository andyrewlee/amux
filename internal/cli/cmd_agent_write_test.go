package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestCmdAgentStopMissingSessionReturnsNotFound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origStateFor := tmuxSessionStateFor
	origKill := tmuxKillSession
	defer func() {
		tmuxSessionStateFor = origStateFor
		tmuxKillSession = origKill
	}()

	killCalled := false
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: false}, nil
	}
	tmuxKillSession = func(_ string, _ tmux.Options) error {
		killCalled = true
		return nil
	}

	var out, errOut bytes.Buffer
	code := cmdAgentStop(&out, &errOut, GlobalFlags{JSON: true}, []string{"missing-session"}, "test-v1")
	if code != ExitNotFound {
		t.Fatalf("cmdAgentStop() code = %d, want %d", code, ExitNotFound)
	}
	if killCalled {
		t.Fatalf("expected tmuxKillSession to not be called when session is missing")
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false for missing session")
	}
	if env.Error == nil || env.Error.Code != "not_found" {
		t.Fatalf("expected error code not_found, got %#v", env.Error)
	}
}

func TestCmdAgentStopSessionLookupErrorReturnsInternalError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origStateFor := tmuxSessionStateFor
	origKill := tmuxKillSession
	defer func() {
		tmuxSessionStateFor = origStateFor
		tmuxKillSession = origKill
	}()

	killCalled := false
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{}, errors.New("tmux down")
	}
	tmuxKillSession = func(_ string, _ tmux.Options) error {
		killCalled = true
		return nil
	}

	var out, errOut bytes.Buffer
	code := cmdAgentStop(&out, &errOut, GlobalFlags{JSON: true}, []string{"any-session"}, "test-v1")
	if code != ExitInternalError {
		t.Fatalf("cmdAgentStop() code = %d, want %d", code, ExitInternalError)
	}
	if killCalled {
		t.Fatalf("expected tmuxKillSession to not be called when session lookup fails")
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false for lookup failure")
	}
	if env.Error == nil || env.Error.Code != "stop_failed" {
		t.Fatalf("expected error code stop_failed, got %#v", env.Error)
	}
}

func TestCmdAgentRunUsageJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cmdAgentRun(&out, &errOut, GlobalFlags{JSON: true}, nil, "test-v1")
	if code != ExitUsage {
		t.Fatalf("cmdAgentRun() code = %d, want %d", code, ExitUsage)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
}

func TestCmdAgentRunRejectsInvalidWorkspaceID(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cmdAgentRun(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"--workspace", "../../../tmp/evil"},
		"test-v1",
	)
	if code != ExitUsage {
		t.Fatalf("cmdAgentRun() code = %d, want %d", code, ExitUsage)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
	if env.Error == nil || !strings.Contains(env.Error.Message, "invalid workspace id") {
		t.Fatalf("expected invalid workspace id message, got %#v", env.Error)
	}
}

func TestCmdAgentRunRejectsUnexpectedPositionalArguments(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cmdAgentRun(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"--workspace", "0123456789abcdef", "stray-token"},
		"test-v1",
	)
	if code != ExitUsage {
		t.Fatalf("cmdAgentRun() code = %d, want %d", code, ExitUsage)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
	if env.Error == nil || !strings.Contains(env.Error.Message, "unexpected arguments") {
		t.Fatalf("expected unexpected arguments message, got %#v", env.Error)
	}
}

func TestCmdAgentSendParseErrorJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cmdAgentSend(&out, &errOut, GlobalFlags{JSON: true}, []string{"session", "--bad-flag"}, "test-v1")
	if code != ExitUsage {
		t.Fatalf("cmdAgentSend() code = %d, want %d", code, ExitUsage)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
	if env.Error == nil || !strings.Contains(env.Error.Message, "flag provided but not defined") {
		t.Fatalf("expected parse error message, got %#v", env.Error)
	}
}

func TestCmdAgentSendJSONJobResultAndIdempotentReplay(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origStateFor := tmuxSessionStateFor
	origSend := tmuxSendKeys
	defer func() {
		tmuxSessionStateFor = origStateFor
		tmuxSendKeys = origSend
	}()

	stateCalls := 0
	sendCalls := 0
	tmuxSessionStateFor = func(name string, _ tmux.Options) (tmux.SessionState, error) {
		stateCalls++
		if name != "session-a" {
			return tmux.SessionState{}, fmt.Errorf("unexpected session %s", name)
		}
		return tmux.SessionState{Exists: true}, nil
	}
	tmuxSendKeys = func(name, text string, enter bool, _ tmux.Options) error {
		sendCalls++
		if name != "session-a" || text != "hello" || !enter {
			return fmt.Errorf("unexpected send args name=%s text=%s enter=%v", name, text, enter)
		}
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	args := []string{"session-a", "--text", "hello", "--enter", "--idempotency-key", "idem-send-ok"}
	code := cmdAgentSend(&out, &errOut, GlobalFlags{JSON: true}, args, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdAgentSend() code = %d, want %d", code, ExitOK)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr in JSON mode, got %q", errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got error=%#v", env.Error)
	}
	payload, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", env.Data)
	}
	if got, _ := payload["status"].(string); got != string(sendJobCompleted) {
		t.Fatalf("status = %q, want %q", got, sendJobCompleted)
	}
	if got, _ := payload["sent"].(bool); !got {
		t.Fatalf("sent = %v, want true", got)
	}
	jobID, _ := payload["job_id"].(string)
	if jobID == "" {
		t.Fatalf("expected non-empty job_id in response")
	}
	if sendCalls != 1 {
		t.Fatalf("send calls = %d, want 1", sendCalls)
	}

	var replay bytes.Buffer
	var replayErr bytes.Buffer
	replayCode := cmdAgentSend(&replay, &replayErr, GlobalFlags{JSON: true}, args, "test-v1")
	if replayCode != ExitOK {
		t.Fatalf("replay code = %d, want %d", replayCode, ExitOK)
	}
	if replayErr.Len() != 0 {
		t.Fatalf("expected empty replay stderr, got %q", replayErr.String())
	}
	if replay.String() != out.String() {
		t.Fatalf("replayed output mismatch\nfirst:\n%s\nreplay:\n%s", out.String(), replay.String())
	}
	if sendCalls != 1 {
		t.Fatalf("send calls after replay = %d, want 1", sendCalls)
	}
	if stateCalls != 1 {
		t.Fatalf("state checks after replay = %d, want 1", stateCalls)
	}
}

func TestCmdAgentSendIdempotentErrorReplay(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origStateFor := tmuxSessionStateFor
	defer func() {
		tmuxSessionStateFor = origStateFor
	}()

	stateCalls := 0
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		stateCalls++
		return tmux.SessionState{Exists: false}, nil
	}

	args := []string{"missing-session", "--text", "hello", "--idempotency-key", "idem-send-not-found"}
	var first bytes.Buffer
	var firstErr bytes.Buffer
	code := cmdAgentSend(&first, &firstErr, GlobalFlags{JSON: true}, args, "test-v1")
	if code != ExitNotFound {
		t.Fatalf("first code = %d, want %d", code, ExitNotFound)
	}
	if firstErr.Len() != 0 {
		t.Fatalf("expected no stderr in JSON mode, got %q", firstErr.String())
	}

	var firstEnv Envelope
	if err := json.Unmarshal(first.Bytes(), &firstEnv); err != nil {
		t.Fatalf("json.Unmarshal(first) error = %v", err)
	}
	if firstEnv.OK || firstEnv.Error == nil || firstEnv.Error.Code != "not_found" {
		t.Fatalf("expected not_found error, got %#v", firstEnv.Error)
	}

	var replay bytes.Buffer
	var replayErr bytes.Buffer
	replayCode := cmdAgentSend(&replay, &replayErr, GlobalFlags{JSON: true}, args, "test-v1")
	if replayCode != ExitNotFound {
		t.Fatalf("replay code = %d, want %d", replayCode, ExitNotFound)
	}
	if replayErr.Len() != 0 {
		t.Fatalf("expected empty replay stderr, got %q", replayErr.String())
	}
	if replay.String() != first.String() {
		t.Fatalf("replayed output mismatch\nfirst:\n%s\nreplay:\n%s", first.String(), replay.String())
	}
	if stateCalls != 1 {
		t.Fatalf("state checks after replay = %d, want 1", stateCalls)
	}
}

func TestCmdAgentStopGracefulFallbackKillsWhenStillRunning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origStateFor := tmuxSessionStateFor
	origInterrupt := tmuxSendInterrupt
	origKill := tmuxKillSession
	defer func() {
		tmuxSessionStateFor = origStateFor
		tmuxSendInterrupt = origInterrupt
		tmuxKillSession = origKill
	}()

	stateChecks := 0
	interruptCalls := 0
	killCalls := 0
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		stateChecks++
		return tmux.SessionState{Exists: true}, nil
	}
	tmuxSendInterrupt = func(_ string, _ tmux.Options) error {
		interruptCalls++
		return nil
	}
	tmuxKillSession = func(_ string, _ tmux.Options) error {
		killCalls++
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdAgentStop(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"session-a", "--grace-period", "10ms"},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdAgentStop() code = %d, want %d", code, ExitOK)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}
	if interruptCalls != 1 {
		t.Fatalf("interrupt calls = %d, want 1", interruptCalls)
	}
	if killCalls != 1 {
		t.Fatalf("kill calls = %d, want 1", killCalls)
	}
	if stateChecks < 2 {
		t.Fatalf("state checks = %d, want >= 2 (precheck + graceful polling)", stateChecks)
	}
}

func TestCmdAgentStopGracefulNoKillWhenSessionExits(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origStateFor := tmuxSessionStateFor
	origInterrupt := tmuxSendInterrupt
	origKill := tmuxKillSession
	defer func() {
		tmuxSessionStateFor = origStateFor
		tmuxSendInterrupt = origInterrupt
		tmuxKillSession = origKill
	}()

	stateChecks := 0
	interruptCalls := 0
	killCalls := 0
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		stateChecks++
		if stateChecks == 1 {
			return tmux.SessionState{Exists: true}, nil
		}
		return tmux.SessionState{Exists: false}, nil
	}
	tmuxSendInterrupt = func(_ string, _ tmux.Options) error {
		interruptCalls++
		return nil
	}
	tmuxKillSession = func(_ string, _ tmux.Options) error {
		killCalls++
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdAgentStop(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"session-a", "--grace-period", (200 * time.Millisecond).String()},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdAgentStop() code = %d, want %d", code, ExitOK)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}
	if interruptCalls != 1 {
		t.Fatalf("interrupt calls = %d, want 1", interruptCalls)
	}
	if killCalls != 0 {
		t.Fatalf("kill calls = %d, want 0", killCalls)
	}
}
