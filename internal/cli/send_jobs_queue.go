package cli

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	sendJobQueuePollInterval    = 20 * time.Millisecond
	sendJobQueueMaxPollInterval = 1 * time.Second
	sendJobQueueMaxWait         = 2 * sendJobsStaleAfter
)

func (s *sendJobStore) queueLockPath(sessionName string) string {
	sum := sha1.Sum([]byte(sessionName))
	return filepath.Join(filepath.Dir(s.path), "cli-send-queue-"+hex.EncodeToString(sum[:8])+".lock")
}

func waitForSessionQueueTurnForJob(store *sendJobStore, sessionName, jobID string) (*os.File, error) {
	jobID = strings.TrimSpace(jobID)
	start := time.Now()
	delay := sendJobQueuePollInterval
	for {
		lockFile, err := lockIdempotencyFile(store.queueLockPath(sessionName), false)
		if err != nil {
			return nil, err
		}
		if jobID == "" {
			return lockFile, nil
		}

		queued, err := store.isQueuedJobForSession(sessionName, jobID)
		if err != nil {
			unlockIdempotencyFile(lockFile)
			return nil, err
		}
		if !queued {
			return lockFile, nil
		}

		head, ok, err := store.nextQueuedJobForSession(sessionName)
		if err != nil {
			unlockIdempotencyFile(lockFile)
			return nil, err
		}
		if !ok || head.ID == jobID {
			return lockFile, nil
		}

		unlockIdempotencyFile(lockFile)
		// Polling keeps the cross-process queue lock simple and portable.
		// A bounded max wait prevents orphaned processors from spinning forever.
		if sendJobQueueMaxWait > 0 && time.Since(start) >= sendJobQueueMaxWait {
			return nil, fmt.Errorf("timed out waiting for send queue turn for job %s", jobID)
		}
		time.Sleep(delay)
		// Exponential backoff: double the delay each iteration, capped at 1s.
		delay *= 2
		if delay > sendJobQueueMaxPollInterval {
			delay = sendJobQueueMaxPollInterval
		}
	}
}

func releaseSessionQueueTurn(lockFile *os.File) {
	unlockIdempotencyFile(lockFile)
}

func (s *sendJobStore) isQueuedJobForSession(sessionName, jobID string) (bool, error) {
	lockFile, err := lockIdempotencyFile(s.lockPath(), false)
	if err != nil {
		return false, err
	}
	defer unlockIdempotencyFile(lockFile)

	state, err := s.loadState()
	if err != nil {
		return false, err
	}
	if s.reconcileStale(state) {
		if err := s.saveState(state); err != nil {
			return false, err
		}
	}

	job, ok := state.Jobs[jobID]
	if !ok {
		return false, nil
	}
	if strings.TrimSpace(job.SessionName) != strings.TrimSpace(sessionName) {
		return false, nil
	}
	return isQueuedSendJobStatus(job.Status), nil
}

func (s *sendJobStore) nextQueuedJobForSession(sessionName string) (sendJob, bool, error) {
	lockFile, err := lockIdempotencyFile(s.lockPath(), false)
	if err != nil {
		return sendJob{}, false, err
	}
	defer unlockIdempotencyFile(lockFile)

	state, err := s.loadState()
	if err != nil {
		return sendJob{}, false, err
	}
	if s.reconcileStale(state) {
		if err := s.saveState(state); err != nil {
			return sendJob{}, false, err
		}
	}

	target := strings.TrimSpace(sessionName)
	var head sendJob
	var found bool
	for _, job := range state.Jobs {
		if strings.TrimSpace(job.SessionName) != target {
			continue
		}
		if !isQueuedSendJobStatus(job.Status) {
			continue
		}
		if !found || sendJobComesBefore(job, head) {
			head = job
			found = true
		}
	}
	return head, found, nil
}

func isQueuedSendJobStatus(status sendJobStatus) bool {
	return status == sendJobPending || status == sendJobRunning
}

func sendJobComesBefore(candidate, existing sendJob) bool {
	if existing.Status == sendJobRunning && candidate.Status != sendJobRunning {
		return false
	}
	if candidate.Status == sendJobRunning && existing.Status != sendJobRunning {
		return true
	}
	if candidate.CreatedAt < existing.CreatedAt {
		return true
	}
	if candidate.CreatedAt > existing.CreatedAt {
		return false
	}
	if candidate.Sequence > 0 && existing.Sequence > 0 && candidate.Sequence != existing.Sequence {
		return candidate.Sequence < existing.Sequence
	}
	return candidate.ID < existing.ID
}
