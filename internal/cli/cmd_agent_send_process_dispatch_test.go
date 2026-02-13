package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestCmdAgentSendSessionLookupErrorReturnsInternalError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origStateFor := tmuxSessionStateFor
	defer func() {
		tmuxSessionStateFor = origStateFor
	}()

	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{}, errors.New("tmux lookup timeout")
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdAgentSend(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"session-a", "--text", "hello"},
		"test-v1",
	)
	if code != ExitInternalError {
		t.Fatalf("cmdAgentSend() code = %d, want %d", code, ExitInternalError)
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
	if env.Error == nil || env.Error.Code != "session_lookup_failed" {
		t.Fatalf("expected session_lookup_failed, got %#v", env.Error)
	}
}

func TestCmdAgentSendSessionNotFoundMarksNewJobFailed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := newSendJobStore()
	if err != nil {
		t.Fatalf("newSendJobStore() error = %v", err)
	}

	origStateFor := tmuxSessionStateFor
	defer func() {
		tmuxSessionStateFor = origStateFor
	}()
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: false}, nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdAgentSend(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"session-a", "--text", "hello"},
		"test-v1",
	)
	if code != ExitNotFound {
		t.Fatalf("cmdAgentSend() code = %d, want %d", code, ExitNotFound)
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

	lockFile, err := lockIdempotencyFile(store.lockPath(), false)
	if err != nil {
		t.Fatalf("lockIdempotencyFile() error = %v", err)
	}
	state, err := store.loadState()
	unlockIdempotencyFile(lockFile)
	if err != nil {
		t.Fatalf("store.loadState() error = %v", err)
	}
	if len(state.Jobs) != 1 {
		t.Fatalf("jobs count = %d, want 1", len(state.Jobs))
	}
	var created sendJob
	for _, job := range state.Jobs {
		created = job
		break
	}
	if created.Status != sendJobFailed {
		t.Fatalf("status = %q, want %q", created.Status, sendJobFailed)
	}
	if !strings.Contains(created.Error, "session not found") {
		t.Fatalf("error = %q, want session not found message", created.Error)
	}
	if isQueuedSendJobStatus(created.Status) {
		t.Fatalf("expected terminal failed status, got queued status %q", created.Status)
	}
}
