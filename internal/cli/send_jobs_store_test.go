package cli

import (
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
		t.Fatalf("status = %q, want %q after read-path reconciliation", got.Status, sendJobFailed)
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
		t.Fatalf("status = %q, want %q after read-path reconciliation", got.Status, sendJobFailed)
	}
	if !strings.Contains(got.Error, "stale running timeout") {
		t.Fatalf("error = %q, want stale running timeout message", got.Error)
	}
	if got.CompletedAt == 0 {
		t.Fatalf("expected completed_at to be set for reconciled stale job")
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
