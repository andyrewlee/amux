package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCmdAgentJobStatusAndCancelJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := newSendJobStore()
	if err != nil {
		t.Fatalf("newSendJobStore() error = %v", err)
	}
	job, err := store.create("session-a", "ws-a:tab-a")
	if err != nil {
		t.Fatalf("store.create() error = %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	statusCode := cmdAgentJobStatus(&out, &errOut, GlobalFlags{JSON: true}, []string{job.ID}, "test-v1")
	if statusCode != ExitOK {
		t.Fatalf("cmdAgentJobStatus() code = %d, want %d", statusCode, ExitOK)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}

	var statusEnv Envelope
	if err := json.Unmarshal(out.Bytes(), &statusEnv); err != nil {
		t.Fatalf("json.Unmarshal(status) error = %v", err)
	}
	if !statusEnv.OK {
		t.Fatalf("expected status ok=true, got error=%#v", statusEnv.Error)
	}
	statusData, ok := statusEnv.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected status payload map, got %T", statusEnv.Data)
	}
	if got, _ := statusData["status"].(string); got != string(sendJobPending) {
		t.Fatalf("status = %q, want %q", got, sendJobPending)
	}

	out.Reset()
	errOut.Reset()
	cancelCode := cmdAgentJobCancel(&out, &errOut, GlobalFlags{JSON: true}, []string{job.ID}, "test-v1")
	if cancelCode != ExitOK {
		t.Fatalf("cmdAgentJobCancel() code = %d, want %d", cancelCode, ExitOK)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}

	var cancelEnv Envelope
	if err := json.Unmarshal(out.Bytes(), &cancelEnv); err != nil {
		t.Fatalf("json.Unmarshal(cancel) error = %v", err)
	}
	if !cancelEnv.OK {
		t.Fatalf("expected cancel ok=true, got error=%#v", cancelEnv.Error)
	}
	cancelData, ok := cancelEnv.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected cancel payload map, got %T", cancelEnv.Data)
	}
	if got, _ := cancelData["canceled"].(bool); !got {
		t.Fatalf("canceled = %v, want true", got)
	}
	if got, _ := cancelData["status"].(string); got != string(sendJobCanceled) {
		t.Fatalf("status after cancel = %q, want %q", got, sendJobCanceled)
	}
}

func TestCmdAgentJobCancelRunningReturnsNoop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := newSendJobStore()
	if err != nil {
		t.Fatalf("newSendJobStore() error = %v", err)
	}
	job, err := store.create("session-a", "")
	if err != nil {
		t.Fatalf("store.create() error = %v", err)
	}
	if _, err := store.setStatus(job.ID, sendJobRunning, ""); err != nil {
		t.Fatalf("store.setStatus() error = %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdAgentJobCancel(&out, &errOut, GlobalFlags{JSON: true}, []string{job.ID}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdAgentJobCancel() code = %d, want %d", code, ExitOK)
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
	payload, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected payload map, got %T", env.Data)
	}
	if got, _ := payload["canceled"].(bool); got {
		t.Fatalf("canceled = %v, want false", got)
	}
	if got, _ := payload["status"].(string); got != string(sendJobRunning) {
		t.Fatalf("status = %q, want %q", got, sendJobRunning)
	}
}

func TestCmdAgentJobCancelJSONIdempotentReplay(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := newSendJobStore()
	if err != nil {
		t.Fatalf("newSendJobStore() error = %v", err)
	}
	job, err := store.create("session-a", "")
	if err != nil {
		t.Fatalf("store.create() error = %v", err)
	}

	args := []string{job.ID, "--idempotency-key", "idem-job-cancel-1"}
	var first bytes.Buffer
	var firstErr bytes.Buffer
	code := cmdAgentJobCancel(&first, &firstErr, GlobalFlags{JSON: true}, args, "test-v1")
	if code != ExitOK {
		t.Fatalf("first cmdAgentJobCancel() code = %d, want %d", code, ExitOK)
	}
	if firstErr.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", firstErr.String())
	}

	var replay bytes.Buffer
	var replayErr bytes.Buffer
	replayCode := cmdAgentJobCancel(&replay, &replayErr, GlobalFlags{JSON: true}, args, "test-v1")
	if replayCode != ExitOK {
		t.Fatalf("replay cmdAgentJobCancel() code = %d, want %d", replayCode, ExitOK)
	}
	if replayErr.Len() != 0 {
		t.Fatalf("expected no replay stderr output in JSON mode, got %q", replayErr.String())
	}
	if replay.String() != first.String() {
		t.Fatalf("replayed output mismatch\nfirst:\n%s\nreplay:\n%s", first.String(), replay.String())
	}
}

func TestCmdAgentJobWaitCompletedReturnsOK(t *testing.T) {
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

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdAgentJobWait(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{job.ID, "--timeout", "1s", "--interval", "10ms"},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdAgentJobWait() code = %d, want %d", code, ExitOK)
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
	payload, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected payload map, got %T", env.Data)
	}
	if got, _ := payload["status"].(string); got != string(sendJobCompleted) {
		t.Fatalf("status = %q, want %q", got, sendJobCompleted)
	}
}

func TestCmdAgentJobWaitTimeoutReturnsError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := newSendJobStore()
	if err != nil {
		t.Fatalf("newSendJobStore() error = %v", err)
	}
	job, err := store.create("session-a", "")
	if err != nil {
		t.Fatalf("store.create() error = %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdAgentJobWait(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{job.ID, "--timeout", "40ms", "--interval", "10ms"},
		"test-v1",
	)
	if code != ExitInternalError {
		t.Fatalf("cmdAgentJobWait() code = %d, want %d", code, ExitInternalError)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false for timeout")
	}
	if env.Error == nil || env.Error.Code != "timeout" {
		t.Fatalf("expected timeout error, got %#v", env.Error)
	}
}
