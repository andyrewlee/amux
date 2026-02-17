package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestCmdAgentStopInvalidAgentIDReturnsUsage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var out, errOut bytes.Buffer
	code := cmdAgentStop(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"--agent", "invalid-id"},
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
	if env.Error == nil || env.Error.Code != "invalid_agent_id" {
		t.Fatalf("expected invalid_agent_id, got %#v", env.Error)
	}
}

func TestCmdAgentStopStaleAgentIDReturnsNotFound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origSessionsWithTags := tmuxSessionsWithTagsForAgentID
	defer func() {
		tmuxSessionsWithTagsForAgentID = origSessionsWithTags
	}()

	tmuxSessionsWithTagsForAgentID = func(
		_ map[string]string,
		_ []string,
		_ tmux.Options,
	) ([]tmux.SessionTagValues, error) {
		return nil, nil
	}

	var out, errOut bytes.Buffer
	code := cmdAgentStop(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"--agent", "ws-a:tab-a"},
		"test-v1",
	)
	if code != ExitNotFound {
		t.Fatalf("cmdAgentStop() code = %d, want %d", code, ExitNotFound)
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
