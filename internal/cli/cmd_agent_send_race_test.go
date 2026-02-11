package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestCmdAgentSendProcessJobPreservesFIFOOrder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := newSendJobStore()
	if err != nil {
		t.Fatalf("newSendJobStore() error = %v", err)
	}
	firstJob, err := store.create("session-a", "")
	if err != nil {
		t.Fatalf("store.create(first) error = %v", err)
	}
	secondJob, err := store.create("session-a", "")
	if err != nil {
		t.Fatalf("store.create(second) error = %v", err)
	}
	if err := normalizeJobCreatedAt(store, firstJob.ID, secondJob.ID); err != nil {
		t.Fatalf("normalizeJobCreatedAt() error = %v", err)
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

	var (
		mu   sync.Mutex
		sent []string
	)
	tmuxSendKeys = func(_, text string, _ bool, _ tmux.Options) error {
		mu.Lock()
		sent = append(sent, text)
		mu.Unlock()
		return nil
	}

	var codeFirst, codeSecond int
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		var out, errOut bytes.Buffer
		codeSecond = cmdAgentSend(
			&out,
			&errOut,
			GlobalFlags{JSON: true},
			[]string{"session-a", "--text", "second", "--process-job", "--job-id", secondJob.ID},
			"test-v1",
		)
	}()
	go func() {
		defer wg.Done()
		var out, errOut bytes.Buffer
		codeFirst = cmdAgentSend(
			&out,
			&errOut,
			GlobalFlags{JSON: true},
			[]string{"session-a", "--text", "first", "--process-job", "--job-id", firstJob.ID},
			"test-v1",
		)
	}()
	wg.Wait()

	if codeFirst != ExitOK {
		t.Fatalf("first job code = %d, want %d", codeFirst, ExitOK)
	}
	if codeSecond != ExitOK {
		t.Fatalf("second job code = %d, want %d", codeSecond, ExitOK)
	}

	if len(sent) != 2 {
		t.Fatalf("sent count = %d, want 2 (sent=%v)", len(sent), sent)
	}
	if sent[0] != "first" || sent[1] != "second" {
		t.Fatalf("send order = %v, want [first second]", sent)
	}
}

func normalizeJobCreatedAt(store *sendJobStore, firstID, secondID string) error {
	lockFile, err := lockIdempotencyFile(store.lockPath(), false)
	if err != nil {
		return err
	}
	defer unlockIdempotencyFile(lockFile)

	state, err := store.loadState()
	if err != nil {
		return err
	}
	first := state.Jobs[firstID]
	second := state.Jobs[secondID]
	now := time.Now().Unix()
	first.Sequence = 1
	first.CreatedAt = now
	first.UpdatedAt = now
	second.Sequence = 2
	second.CreatedAt = now
	second.UpdatedAt = now
	state.NextSequence = 2
	state.Jobs[firstID] = first
	state.Jobs[secondID] = second
	return store.saveState(state)
}
