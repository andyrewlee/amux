package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestCmdAgentSendAsyncJSONEnqueuesAndReplaysIdempotently(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origStateFor := tmuxSessionStateFor
	origSend := tmuxSendKeys
	origLauncher := startSendJobProcess
	defer func() {
		tmuxSessionStateFor = origStateFor
		tmuxSendKeys = origSend
		startSendJobProcess = origLauncher
	}()

	stateChecks := 0
	sendCalls := 0
	launchCalls := 0
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		stateChecks++
		return tmux.SessionState{Exists: true}, nil
	}
	tmuxSendKeys = func(_, _ string, _ bool, _ tmux.Options) error {
		sendCalls++
		return nil
	}
	startSendJobProcess = func(args sendJobProcessArgs) error {
		launchCalls++
		if args.JobID == "" {
			t.Fatalf("expected async launcher to receive job id")
		}
		return nil
	}

	args := []string{"session-a", "--text", "hello", "--async", "--idempotency-key", "idem-async-1"}

	var firstOut bytes.Buffer
	var firstErr bytes.Buffer
	firstCode := cmdAgentSend(&firstOut, &firstErr, GlobalFlags{JSON: true}, args, "test-v1")
	if firstCode != ExitOK {
		t.Fatalf("first code = %d, want %d", firstCode, ExitOK)
	}
	if firstErr.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", firstErr.String())
	}

	var firstEnv Envelope
	if err := json.Unmarshal(firstOut.Bytes(), &firstEnv); err != nil {
		t.Fatalf("json.Unmarshal(first) error = %v", err)
	}
	if !firstEnv.OK {
		t.Fatalf("expected ok=true, got error=%#v", firstEnv.Error)
	}
	firstData, ok := firstEnv.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T", firstEnv.Data)
	}
	if got, _ := firstData["status"].(string); got != string(sendJobPending) {
		t.Fatalf("status = %q, want %q", got, sendJobPending)
	}
	if got, _ := firstData["sent"].(bool); got {
		t.Fatalf("sent = %v, want false", got)
	}
	if got, _ := firstData["job_id"].(string); got == "" {
		t.Fatalf("expected non-empty job_id")
	}
	if launchCalls != 1 {
		t.Fatalf("launch calls = %d, want 1", launchCalls)
	}
	if sendCalls != 0 {
		t.Fatalf("send calls = %d, want 0 for async enqueue", sendCalls)
	}

	var replayOut bytes.Buffer
	var replayErr bytes.Buffer
	replayCode := cmdAgentSend(&replayOut, &replayErr, GlobalFlags{JSON: true}, args, "test-v1")
	if replayCode != ExitOK {
		t.Fatalf("replay code = %d, want %d", replayCode, ExitOK)
	}
	if replayErr.Len() != 0 {
		t.Fatalf("expected no replay stderr output, got %q", replayErr.String())
	}
	if replayOut.String() != firstOut.String() {
		t.Fatalf("replay output mismatch\nfirst:\n%s\nreplay:\n%s", firstOut.String(), replayOut.String())
	}
	if launchCalls != 1 {
		t.Fatalf("launch calls after replay = %d, want 1", launchCalls)
	}
	if stateChecks != 1 {
		t.Fatalf("state checks after replay = %d, want 1", stateChecks)
	}
}
