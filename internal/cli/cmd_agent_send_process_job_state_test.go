package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestCmdAgentSendProcessJobLookupFailureMarksJobFailed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := newSendJobStore()
	if err != nil {
		t.Fatalf("newSendJobStore() error = %v", err)
	}
	job, err := store.create("session-a", "")
	if err != nil {
		t.Fatalf("store.create() error = %v", err)
	}

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
		[]string{
			"session-a",
			"--text", "hello",
			"--process-job",
			"--job-id", job.ID,
		},
		"test-v1",
	)
	if code != ExitInternalError {
		t.Fatalf("cmdAgentSend() code = %d, want %d", code, ExitInternalError)
	}

	got, ok, err := store.get(job.ID)
	if err != nil {
		t.Fatalf("store.get() error = %v", err)
	}
	if !ok {
		t.Fatalf("expected job to exist")
	}
	if got.Status != sendJobFailed {
		t.Fatalf("status = %q, want %q", got.Status, sendJobFailed)
	}
	if !strings.Contains(got.Error, "session lookup failed") {
		t.Fatalf("error = %q, want session lookup failure message", got.Error)
	}
}

func TestCmdAgentSendProcessJobCompletedDoesNotSendAgain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := newSendJobStore()
	if err != nil {
		t.Fatalf("newSendJobStore() error = %v", err)
	}
	job, err := store.create("session-a", "")
	if err != nil {
		t.Fatalf("store.create() error = %v", err)
	}
	if _, err := store.setStatus(job.ID, sendJobCompleted, ""); err != nil {
		t.Fatalf("store.setStatus() error = %v", err)
	}

	origStateFor := tmuxSessionStateFor
	origSend := tmuxSendKeys
	defer func() {
		tmuxSessionStateFor = origStateFor
		tmuxSendKeys = origSend
	}()
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true}, nil
	}

	sendCalls := 0
	tmuxSendKeys = func(_, _ string, _ bool, _ tmux.Options) error {
		sendCalls++
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdAgentSend(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{
			"session-a",
			"--text", "hello",
			"--process-job",
			"--job-id", job.ID,
		},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdAgentSend() code = %d, want %d", code, ExitOK)
	}
	if sendCalls != 0 {
		t.Fatalf("tmuxSendKeys calls = %d, want 0", sendCalls)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
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
		t.Fatalf("expected response data object, got %T", env.Data)
	}
	if got, _ := data["status"].(string); got != string(sendJobCompleted) {
		t.Fatalf("status = %q, want %q", got, sendJobCompleted)
	}
	if got, _ := data["sent"].(bool); !got {
		t.Fatalf("sent = %v, want true", got)
	}
}

func TestCmdAgentSendProcessJobValidatesStoredSessionName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := newSendJobStore()
	if err != nil {
		t.Fatalf("newSendJobStore() error = %v", err)
	}
	job, err := store.create("session-missing", "")
	if err != nil {
		t.Fatalf("store.create() error = %v", err)
	}

	origStateFor := tmuxSessionStateFor
	origSend := tmuxSendKeys
	defer func() {
		tmuxSessionStateFor = origStateFor
		tmuxSendKeys = origSend
	}()
	tmuxSessionStateFor = func(name string, _ tmux.Options) (tmux.SessionState, error) {
		if name == "session-positional" {
			return tmux.SessionState{Exists: true}, nil
		}
		return tmux.SessionState{Exists: false}, nil
	}
	sendCalls := 0
	tmuxSendKeys = func(_, _ string, _ bool, _ tmux.Options) error {
		sendCalls++
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdAgentSend(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{
			"session-positional",
			"--text", "hello",
			"--process-job",
			"--job-id", job.ID,
		},
		"test-v1",
	)
	if code != ExitNotFound {
		t.Fatalf("cmdAgentSend() code = %d, want %d", code, ExitNotFound)
	}
	if sendCalls != 0 {
		t.Fatalf("tmuxSendKeys calls = %d, want 0", sendCalls)
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
	if env.Error == nil || !strings.Contains(env.Error.Message, "session session-missing not found") {
		t.Fatalf("unexpected error message: %#v", env.Error)
	}
}
