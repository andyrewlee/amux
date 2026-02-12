package cli

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

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
