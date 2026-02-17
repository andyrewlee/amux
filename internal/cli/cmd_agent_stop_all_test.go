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

func TestCmdAgentStopAllIncludesPartiallyTaggedSessionsFromTagQuery(t *testing.T) {
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
					// @amux_type intentionally missing to model partial tag writes.
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
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}
	if got := killed["session-partial"]; got != 1 {
		t.Fatalf("session-partial kill calls = %d, want 1", got)
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got error=%#v", env.Error)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T", env.Data)
	}
	stopped, ok := data["stopped"].([]any)
	if !ok || len(stopped) != 1 {
		t.Fatalf("expected one stopped session, got %#v", data["stopped"])
	}
	if got, _ := stopped[0].(string); got != "session-partial" {
		t.Fatalf("stopped[0] = %q, want %q", got, "session-partial")
	}
}
