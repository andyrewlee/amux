package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestCmdAgentSendInvalidAgentIDReturnsUsage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var out, errOut bytes.Buffer
	code := cmdAgentSend(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"--agent", "invalid-id", "--text", "hello"},
		"test-v1",
	)
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
	if env.Error == nil || env.Error.Code != "invalid_agent_id" {
		t.Fatalf("expected invalid_agent_id, got %#v", env.Error)
	}
}

func TestCmdAgentSendStaleAgentIDReturnsNotFound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origSessionsWithTags := tmuxSessionsWithTagsForAgentID
	origStateFor := tmuxSessionStateFor
	defer func() {
		tmuxSessionsWithTagsForAgentID = origSessionsWithTags
		tmuxSessionStateFor = origStateFor
	}()

	tmuxSessionsWithTagsForAgentID = func(
		_ map[string]string,
		_ []string,
		_ tmux.Options,
	) ([]tmux.SessionTagValues, error) {
		return nil, nil
	}
	sessionLookupCalled := false
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		sessionLookupCalled = true
		return tmux.SessionState{Exists: true}, nil
	}

	var out, errOut bytes.Buffer
	code := cmdAgentSend(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"--agent", "ws-a:tab-a", "--text", "hello"},
		"test-v1",
	)
	if code != ExitNotFound {
		t.Fatalf("cmdAgentSend() code = %d, want %d", code, ExitNotFound)
	}
	if sessionLookupCalled {
		t.Fatalf("expected session lookup to be skipped for stale agent IDs")
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
	if env.Error == nil || env.Error.Code != "not_found" {
		t.Fatalf("expected not_found, got %#v", env.Error)
	}
}
