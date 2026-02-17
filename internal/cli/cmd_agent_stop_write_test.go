package cli

import (
	"bytes"
	"encoding/json"
	"errors"
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
