package cli

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestSendJobStoreGetReconcilesStalePendingJob(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := newSendJobStore()
	if err != nil {
		t.Fatalf("newSendJobStore() error = %v", err)
	}
	job, err := store.create("session-a", "")
	if err != nil {
		t.Fatalf("store.create() error = %v", err)
	}

	if err := makeJobStale(store, job.ID, sendJobPending); err != nil {
		t.Fatalf("makeJobStale() error = %v", err)
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
	if !strings.Contains(got.Error, "stale pending timeout") {
		t.Fatalf("error = %q, want stale pending timeout message", got.Error)
	}
	if got.CompletedAt == 0 {
		t.Fatalf("expected completed_at to be set for reconciled stale job")
	}
}

func TestSendJobStoreGetReconcilesStaleRunningJob(t *testing.T) {
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

	if err := makeJobStale(store, job.ID, sendJobRunning); err != nil {
		t.Fatalf("makeJobStale() error = %v", err)
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
	if !strings.Contains(got.Error, "stale running timeout") {
		t.Fatalf("error = %q, want stale running timeout message", got.Error)
	}
}

func TestSendJobStoreGetDoesNotReconcileFreshPending(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := newSendJobStore()
	if err != nil {
		t.Fatalf("newSendJobStore() error = %v", err)
	}
	job, err := store.create("session-a", "")
	if err != nil {
		t.Fatalf("store.create() error = %v", err)
	}

	got, ok, err := store.get(job.ID)
	if err != nil {
		t.Fatalf("store.get() error = %v", err)
	}
	if !ok {
		t.Fatalf("expected job to exist")
	}
	if got.Status != sendJobPending {
		t.Fatalf("status = %q, want %q", got.Status, sendJobPending)
	}
}

func TestSendJobStoreSetStatusDoesNotOverrideCanceledJob(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := newSendJobStore()
	if err != nil {
		t.Fatalf("newSendJobStore() error = %v", err)
	}
	job, err := store.create("session-a", "")
	if err != nil {
		t.Fatalf("store.create() error = %v", err)
	}
	canceledJob, ok, canceled, err := store.cancel(job.ID)
	if err != nil {
		t.Fatalf("store.cancel() error = %v", err)
	}
	if !ok || !canceled {
		t.Fatalf("expected cancel to succeed, ok=%v canceled=%v", ok, canceled)
	}

	updated, err := store.setStatus(job.ID, sendJobRunning, "")
	if err != nil {
		t.Fatalf("store.setStatus() error = %v", err)
	}
	if updated.Status != sendJobCanceled {
		t.Fatalf("status after running transition = %q, want %q", updated.Status, sendJobCanceled)
	}
	if _, err := store.setStatus(job.ID, sendJobFailed, "should-not-overwrite"); err != nil {
		t.Fatalf("store.setStatus() error = %v", err)
	}

	got, exists, err := store.get(job.ID)
	if err != nil {
		t.Fatalf("store.get() error = %v", err)
	}
	if !exists {
		t.Fatalf("expected job to exist")
	}
	if got.Status != sendJobCanceled {
		t.Fatalf("persisted status = %q, want %q", got.Status, sendJobCanceled)
	}
	if got.CompletedAt != canceledJob.CompletedAt {
		t.Fatalf("completed_at changed from %d to %d", canceledJob.CompletedAt, got.CompletedAt)
	}
}

func TestSendJobStoreSetStatusDoesNotReopenCompletedJob(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := newSendJobStore()
	if err != nil {
		t.Fatalf("newSendJobStore() error = %v", err)
	}
	job, err := store.create("session-a", "")
	if err != nil {
		t.Fatalf("store.create() error = %v", err)
	}
	completed, err := store.setStatus(job.ID, sendJobCompleted, "")
	if err != nil {
		t.Fatalf("store.setStatus() error = %v", err)
	}
	if completed.Status != sendJobCompleted {
		t.Fatalf("status = %q, want %q", completed.Status, sendJobCompleted)
	}

	updated, err := store.setStatus(job.ID, sendJobRunning, "")
	if err != nil {
		t.Fatalf("store.setStatus() error = %v", err)
	}
	if updated.Status != sendJobCompleted {
		t.Fatalf("status after running transition = %q, want %q", updated.Status, sendJobCompleted)
	}
}

func TestWaitForSessionQueueTurnForJobReturnsWhenCanceledBehindQueue(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := newSendJobStore()
	if err != nil {
		t.Fatalf("newSendJobStore() error = %v", err)
	}
	running, err := store.create("session-a", "")
	if err != nil {
		t.Fatalf("store.create(running) error = %v", err)
	}
	if _, err := store.setStatus(running.ID, sendJobRunning, ""); err != nil {
		t.Fatalf("store.setStatus(running) error = %v", err)
	}
	canceled, err := store.create("session-a", "")
	if err != nil {
		t.Fatalf("store.create(canceled) error = %v", err)
	}
	trailing, err := store.create("session-a", "")
	if err != nil {
		t.Fatalf("store.create(trailing) error = %v", err)
	}
	if _, ok, didCancel, err := store.cancel(canceled.ID); err != nil {
		t.Fatalf("store.cancel() error = %v", err)
	} else if !ok || !didCancel {
		t.Fatalf("expected cancel to succeed, ok=%v canceled=%v", ok, didCancel)
	}

	type waitResult struct {
		lock *os.File
		err  error
	}
	resultCh := make(chan waitResult, 1)
	go func() {
		lock, waitErr := waitForSessionQueueTurnForJob(store, "session-a", canceled.ID)
		resultCh <- waitResult{lock: lock, err: waitErr}
	}()

	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatalf("waitForSessionQueueTurnForJob() error = %v", result.err)
		}
		if result.lock == nil {
			t.Fatalf("expected queue lock when canceled job exits wait loop")
		}
		releaseSessionQueueTurn(result.lock)
	case <-time.After(250 * time.Millisecond):
		t.Fatal("waitForSessionQueueTurnForJob() blocked for canceled job")
	}

	trailingJob, ok, err := store.get(trailing.ID)
	if err != nil {
		t.Fatalf("store.get(trailing) error = %v", err)
	}
	if !ok {
		t.Fatalf("expected trailing job to exist")
	}
	if trailingJob.Status != sendJobPending {
		t.Fatalf("trailing status = %q, want %q", trailingJob.Status, sendJobPending)
	}
}

func TestSendJobStoreNextQueuedJobUsesSequenceWhenCreatedAtMatches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := newSendJobStore()
	if err != nil {
		t.Fatalf("newSendJobStore() error = %v", err)
	}
	first, err := store.create("session-a", "")
	if err != nil {
		t.Fatalf("store.create(first) error = %v", err)
	}
	second, err := store.create("session-a", "")
	if err != nil {
		t.Fatalf("store.create(second) error = %v", err)
	}

	lockFile, err := lockIdempotencyFile(store.lockPath(), false)
	if err != nil {
		t.Fatalf("lockIdempotencyFile() error = %v", err)
	}
	state, err := store.loadState()
	if err != nil {
		unlockIdempotencyFile(lockFile)
		t.Fatalf("store.loadState() error = %v", err)
	}

	now := time.Now().Unix()
	firstJob := state.Jobs[first.ID]
	secondJob := state.Jobs[second.ID]
	firstJob.CreatedAt = now
	firstJob.UpdatedAt = now
	firstJob.Sequence = 20
	secondJob.CreatedAt = now
	secondJob.UpdatedAt = now
	secondJob.Sequence = 10
	state.NextSequence = 20
	state.Jobs[first.ID] = firstJob
	state.Jobs[second.ID] = secondJob
	if err := store.saveState(state); err != nil {
		unlockIdempotencyFile(lockFile)
		t.Fatalf("store.saveState() error = %v", err)
	}
	unlockIdempotencyFile(lockFile)

	head, ok, err := store.nextQueuedJobForSession("session-a")
	if err != nil {
		t.Fatalf("store.nextQueuedJobForSession() error = %v", err)
	}
	if !ok {
		t.Fatalf("expected queued job head")
	}
	if head.ID != second.ID {
		t.Fatalf("head job = %s, want %s (lower sequence first)", head.ID, second.ID)
	}
}

func TestWaitForSessionQueueTurnForJobTimesOutWhenHeadNeverAdvances(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origPoll := sendJobQueuePollInterval
	origMaxWait := sendJobQueueMaxWait
	sendJobQueuePollInterval = 5 * time.Millisecond
	sendJobQueueMaxWait = 40 * time.Millisecond
	defer func() {
		sendJobQueuePollInterval = origPoll
		sendJobQueueMaxWait = origMaxWait
	}()

	store, err := newSendJobStore()
	if err != nil {
		t.Fatalf("newSendJobStore() error = %v", err)
	}
	head, err := store.create("session-a", "")
	if err != nil {
		t.Fatalf("store.create(head) error = %v", err)
	}
	if _, err := store.setStatus(head.ID, sendJobRunning, ""); err != nil {
		t.Fatalf("store.setStatus(head) error = %v", err)
	}
	follower, err := store.create("session-a", "")
	if err != nil {
		t.Fatalf("store.create(follower) error = %v", err)
	}

	lock, waitErr := waitForSessionQueueTurnForJob(store, "session-a", follower.ID)
	if lock != nil {
		releaseSessionQueueTurn(lock)
		t.Fatalf("expected no lock on timeout")
	}
	if waitErr == nil {
		t.Fatalf("expected timeout waiting for queue turn")
	}
	if !strings.Contains(waitErr.Error(), "timed out waiting for send queue turn") {
		t.Fatalf("unexpected wait error: %v", waitErr)
	}
}

func makeJobStale(store *sendJobStore, jobID string, status sendJobStatus) error {
	lockFile, err := lockIdempotencyFile(store.lockPath(), false)
	if err != nil {
		return err
	}
	defer unlockIdempotencyFile(lockFile)

	state, err := store.loadState()
	if err != nil {
		return err
	}
	job := state.Jobs[jobID]
	job.Status = status
	job.Error = ""
	job.UpdatedAt = time.Now().Add(-sendJobsStaleAfter - time.Minute).Unix()
	job.CompletedAt = 0
	state.Jobs[jobID] = job
	return store.saveState(state)
}
