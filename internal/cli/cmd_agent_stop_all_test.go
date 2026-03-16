package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestCmdAgentStopAllWithPositionalTargetReturnsUsageError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var out, errOut bytes.Buffer
	code := cmdAgentStop(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"session-a", "--all", "--yes"},
		"test-v1",
	)
	if code != ExitUsage {
		t.Fatalf("cmdAgentStop() code = %d, want %d", code, ExitUsage)
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

func TestCmdAgentStopAllWithAgentTargetReturnsUsageError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var out, errOut bytes.Buffer
	code := cmdAgentStop(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"--agent", "ws-a:tab-a", "--all", "--yes"},
		"test-v1",
	)
	if code != ExitUsage {
		t.Fatalf("cmdAgentStop() code = %d, want %d", code, ExitUsage)
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

func TestCmdAgentStopAllPartialFailureReturnsError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origSessionsByActivity := tmuxActiveAgentSessionsByActivity
	origSessionsWithTags := tmuxSessionsWithTags
	origKillSession := tmuxKillSession
	defer func() {
		tmuxActiveAgentSessionsByActivity = origSessionsByActivity
		tmuxSessionsWithTags = origSessionsWithTags
		tmuxKillSession = origKillSession
	}()

	tmuxActiveAgentSessionsByActivity = func(_ time.Duration, _ tmux.Options) ([]tmux.SessionActivity, error) {
		return []tmux.SessionActivity{
			{Name: "session-ok", WorkspaceID: "ws-a", TabID: "tab-a"},
			{Name: "session-fail", WorkspaceID: "ws-a", TabID: "tab-b"},
		}, nil
	}
	tmuxSessionsWithTags = func(_ map[string]string, _ []string, _ tmux.Options) ([]tmux.SessionTagValues, error) {
		return nil, nil
	}
	tmuxKillSession = func(sessionName string, _ tmux.Options) error {
		if sessionName == "session-fail" {
			return errors.New("kill failed")
		}
		return nil
	}

	var out, errOut bytes.Buffer
	code := cmdAgentStop(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"--all", "--yes", "--graceful=false"},
		"test-v1",
	)
	if code != ExitInternalError {
		t.Fatalf("cmdAgentStop() code = %d, want %d", code, ExitInternalError)
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
	if env.Error == nil || env.Error.Code != "stop_partial_failed" {
		t.Fatalf("expected stop_partial_failed, got %#v", env.Error)
	}
	details, ok := env.Error.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected error details object, got %T", env.Error.Details)
	}
	failed, ok := details["failed"].([]any)
	if !ok || len(failed) != 1 {
		t.Fatalf("expected one failed stop entry, got %#v", details["failed"])
	}
}

func TestCmdAgentStopAllConfirmationRequiredDoesNotCacheIdempotencyError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origSessionsByActivity := tmuxActiveAgentSessionsByActivity
	origSessionsWithTags := tmuxSessionsWithTags
	origKillSession := tmuxKillSession
	defer func() {
		tmuxActiveAgentSessionsByActivity = origSessionsByActivity
		tmuxSessionsWithTags = origSessionsWithTags
		tmuxKillSession = origKillSession
	}()

	tmuxActiveAgentSessionsByActivity = func(_ time.Duration, _ tmux.Options) ([]tmux.SessionActivity, error) {
		return []tmux.SessionActivity{
			{Name: "session-a", WorkspaceID: "ws-a", TabID: "tab-a"},
		}, nil
	}
	tmuxSessionsWithTags = func(_ map[string]string, _ []string, _ tmux.Options) ([]tmux.SessionTagValues, error) {
		return nil, nil
	}

	killCalls := 0
	tmuxKillSession = func(sessionName string, _ tmux.Options) error {
		if sessionName != "session-a" {
			t.Fatalf("tmuxKillSession() session = %q, want session-a", sessionName)
		}
		killCalls++
		return nil
	}

	args := []string{"--all", "--idempotency-key", "idem-stop-all-confirm"}

	var firstOut, firstErr bytes.Buffer
	firstCode := cmdAgentStop(&firstOut, &firstErr, GlobalFlags{JSON: true}, args, "test-v1")
	if firstCode != ExitUnsafeBlocked {
		t.Fatalf("first cmdAgentStop() code = %d, want %d", firstCode, ExitUnsafeBlocked)
	}
	if firstErr.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", firstErr.String())
	}
	if killCalls != 0 {
		t.Fatalf("kill calls after unconfirmed request = %d, want 0", killCalls)
	}

	var firstEnv Envelope
	if err := json.Unmarshal(firstOut.Bytes(), &firstEnv); err != nil {
		t.Fatalf("json.Unmarshal(first) error = %v", err)
	}
	if firstEnv.OK {
		t.Fatalf("expected ok=false for unconfirmed stop-all")
	}
	if firstEnv.Error == nil || firstEnv.Error.Code != "confirmation_required" {
		t.Fatalf("expected confirmation_required, got %#v", firstEnv.Error)
	}

	confirmedArgs := []string{"--all", "--yes", "--graceful=false", "--idempotency-key", "idem-stop-all-confirm"}

	var secondOut, secondErr bytes.Buffer
	secondCode := cmdAgentStop(&secondOut, &secondErr, GlobalFlags{JSON: true}, confirmedArgs, "test-v1")
	if secondCode != ExitOK {
		t.Fatalf("confirmed cmdAgentStop() code = %d, want %d", secondCode, ExitOK)
	}
	if secondErr.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", secondErr.String())
	}
	if killCalls != 1 {
		t.Fatalf("kill calls after confirmed retry = %d, want 1", killCalls)
	}

	var secondEnv Envelope
	if err := json.Unmarshal(secondOut.Bytes(), &secondEnv); err != nil {
		t.Fatalf("json.Unmarshal(second) error = %v", err)
	}
	if !secondEnv.OK {
		t.Fatalf("expected ok=true for confirmed stop-all, got %#v", secondEnv.Error)
	}

	var replayOut, replayErr bytes.Buffer
	replayCode := cmdAgentStop(&replayOut, &replayErr, GlobalFlags{JSON: true}, confirmedArgs, "test-v1")
	if replayCode != ExitOK {
		t.Fatalf("replay cmdAgentStop() code = %d, want %d", replayCode, ExitOK)
	}
	if replayErr.Len() != 0 {
		t.Fatalf("expected no replay stderr output in JSON mode, got %q", replayErr.String())
	}
	if replayOut.String() != secondOut.String() {
		t.Fatalf("replayed output mismatch\nfirst confirmed:\n%s\nreplay:\n%s", secondOut.String(), replayOut.String())
	}
	if killCalls != 1 {
		t.Fatalf("kill calls after replay = %d, want 1", killCalls)
	}
}

func TestCmdAgentStopAllExcludesPartiallyTaggedSessionsWithoutType(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origSessionsByActivity := tmuxActiveAgentSessionsByActivity
	origSessionsWithTags := tmuxSessionsWithTags
	origKillSession := tmuxKillSession
	defer func() {
		tmuxActiveAgentSessionsByActivity = origSessionsByActivity
		tmuxSessionsWithTags = origSessionsWithTags
		tmuxKillSession = origKillSession
	}()

	tmuxActiveAgentSessionsByActivity = func(_ time.Duration, _ tmux.Options) ([]tmux.SessionActivity, error) {
		return nil, nil
	}
	tmuxSessionsWithTags = func(_ map[string]string, _ []string, _ tmux.Options) ([]tmux.SessionTagValues, error) {
		return []tmux.SessionTagValues{
			{
				Name: "session-partial",
				Tags: map[string]string{
					"@amux_workspace": "ws-a",
					"@amux_tab":       "tab-a",
					// @amux_type intentionally missing — sessions without
					// explicit type "agent" are no longer included.
				},
			},
		}, nil
	}
	killed := map[string]int{}
	tmuxKillSession = func(sessionName string, _ tmux.Options) error {
		killed[sessionName]++
		return nil
	}

	var out, errOut bytes.Buffer
	code := cmdAgentStop(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"--all", "--yes", "--graceful=false"},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdAgentStop() code = %d, want %d", code, ExitOK)
	}
	if got := killed["session-partial"]; got != 0 {
		t.Fatalf("session-partial kill calls = %d, want 0 (should be excluded without @amux_type=agent)", got)
	}
}
