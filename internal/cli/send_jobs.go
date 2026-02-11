package cli

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/config"
)

const (
	sendJobsFilename     = "cli-send-jobs.json"
	sendJobsStateVersion = 1
	sendJobsRetention    = 7 * 24 * time.Hour
	sendJobsStaleAfter   = 15 * time.Minute
)

type sendJobStatus string

const (
	sendJobPending   sendJobStatus = "pending"
	sendJobRunning   sendJobStatus = "running"
	sendJobCompleted sendJobStatus = "completed"
	sendJobFailed    sendJobStatus = "failed"
	sendJobCanceled  sendJobStatus = "canceled"
)

type sendJob struct {
	ID          string        `json:"id"`
	Command     string        `json:"command"`
	SessionName string        `json:"session_name"`
	AgentID     string        `json:"agent_id,omitempty"`
	Status      sendJobStatus `json:"status"`
	Error       string        `json:"error,omitempty"`
	CreatedAt   int64         `json:"created_at"`
	UpdatedAt   int64         `json:"updated_at"`
	CompletedAt int64         `json:"completed_at,omitempty"`
}

type sendJobState struct {
	Version int                `json:"version"`
	Jobs    map[string]sendJob `json:"jobs"`
}

type sendJobStore struct {
	path string
}

type agentJobResult struct {
	JobID       string `json:"job_id"`
	Status      string `json:"status"`
	SessionName string `json:"session_name,omitempty"`
	AgentID     string `json:"agent_id,omitempty"`
	Error       string `json:"error,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	CompletedAt int64  `json:"completed_at,omitempty"`
}

type agentJobCancelResult struct {
	JobID    string `json:"job_id"`
	Status   string `json:"status"`
	Canceled bool   `json:"canceled"`
}

func newSendJobStore() (*sendJobStore, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, err
	}
	return &sendJobStore{
		path: filepath.Join(paths.Home, sendJobsFilename),
	}, nil
}

func sendJobToResult(job sendJob) agentJobResult {
	return agentJobResult{
		JobID:       job.ID,
		Status:      string(job.Status),
		SessionName: job.SessionName,
		AgentID:     job.AgentID,
		Error:       job.Error,
		CreatedAt:   job.CreatedAt,
		UpdatedAt:   job.UpdatedAt,
		CompletedAt: job.CompletedAt,
	}
}

func (s *sendJobStore) queueLockPath(sessionName string) string {
	sum := sha1.Sum([]byte(sessionName))
	return filepath.Join(filepath.Dir(s.path), "cli-send-queue-"+hex.EncodeToString(sum[:8])+".lock")
}

func (s *sendJobStore) create(sessionName, agentID string) (sendJob, error) {
	lockFile, err := lockIdempotencyFile(s.lockPath(), false)
	if err != nil {
		return sendJob{}, err
	}
	defer unlockIdempotencyFile(lockFile)

	state, err := s.loadState()
	if err != nil {
		return sendJob{}, err
	}
	s.reconcileStale(state)
	s.prune(state)

	now := time.Now().Unix()
	job := sendJob{
		ID:          newSendJobID(),
		Command:     "agent.send",
		SessionName: sessionName,
		AgentID:     agentID,
		Status:      sendJobPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	state.Jobs[job.ID] = job
	if err := s.saveState(state); err != nil {
		return sendJob{}, err
	}
	return job, nil
}

func (s *sendJobStore) get(jobID string) (sendJob, bool, error) {
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
	job, ok := state.Jobs[jobID]
	return job, ok, nil
}

func (s *sendJobStore) cancel(jobID string) (sendJob, bool, bool, error) {
	lockFile, err := lockIdempotencyFile(s.lockPath(), false)
	if err != nil {
		return sendJob{}, false, false, err
	}
	defer unlockIdempotencyFile(lockFile)

	state, err := s.loadState()
	if err != nil {
		return sendJob{}, false, false, err
	}
	if s.reconcileStale(state) {
		if err := s.saveState(state); err != nil {
			return sendJob{}, false, false, err
		}
	}
	job, ok := state.Jobs[jobID]
	if !ok {
		return sendJob{}, false, false, nil
	}
	if job.Status != sendJobPending {
		return job, true, false, nil
	}
	job.Status = sendJobCanceled
	job.UpdatedAt = time.Now().Unix()
	job.CompletedAt = job.UpdatedAt
	state.Jobs[jobID] = job
	if err := s.saveState(state); err != nil {
		return sendJob{}, false, false, err
	}
	return job, true, true, nil
}

func (s *sendJobStore) setStatus(jobID string, status sendJobStatus, errText string) (sendJob, error) {
	lockFile, err := lockIdempotencyFile(s.lockPath(), false)
	if err != nil {
		return sendJob{}, err
	}
	defer unlockIdempotencyFile(lockFile)

	state, err := s.loadState()
	if err != nil {
		return sendJob{}, err
	}
	if s.reconcileStale(state) {
		if err := s.saveState(state); err != nil {
			return sendJob{}, err
		}
	}
	job, ok := state.Jobs[jobID]
	if !ok {
		return sendJob{}, errors.New("job not found")
	}
	if !canTransitionSendJobStatus(job.Status, status) {
		return job, nil
	}
	job.Status = status
	job.Error = strings.TrimSpace(errText)
	job.UpdatedAt = time.Now().Unix()
	if status == sendJobCompleted || status == sendJobFailed || status == sendJobCanceled {
		job.CompletedAt = job.UpdatedAt
	}
	state.Jobs[jobID] = job
	if err := s.saveState(state); err != nil {
		return sendJob{}, err
	}
	return job, nil
}

func (s *sendJobStore) lockPath() string {
	return s.path + ".lock"
}

func (s *sendJobStore) loadState() (*sendJobState, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return &sendJobState{
			Version: sendJobsStateVersion,
			Jobs:    map[string]sendJob{},
		}, nil
	}
	if err != nil {
		return nil, err
	}

	var state sendJobState
	if err := json.Unmarshal(data, &state); err != nil || state.Version != sendJobsStateVersion {
		return &sendJobState{
			Version: sendJobsStateVersion,
			Jobs:    map[string]sendJob{},
		}, nil
	}
	if state.Jobs == nil {
		state.Jobs = map[string]sendJob{}
	}
	return &state, nil
}

func (s *sendJobStore) saveState(state *sendJobState) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func (s *sendJobStore) prune(state *sendJobState) {
	if state == nil || len(state.Jobs) == 0 {
		return
	}
	cutoff := time.Now().Add(-sendJobsRetention).Unix()
	for id, job := range state.Jobs {
		if job.Status == sendJobPending || job.Status == sendJobRunning {
			continue
		}
		if job.UpdatedAt <= cutoff {
			delete(state.Jobs, id)
		}
	}
}

func (s *sendJobStore) reconcileStale(state *sendJobState) bool {
	if state == nil || len(state.Jobs) == 0 {
		return false
	}
	now := time.Now().Unix()
	staleCutoff := now - int64(sendJobsStaleAfter/time.Second)
	changed := false
	for id, job := range state.Jobs {
		if job.Status != sendJobPending && job.Status != sendJobRunning {
			continue
		}
		if job.UpdatedAt > staleCutoff {
			continue
		}
		original := job.Status
		job.Status = sendJobFailed
		if job.Error == "" {
			job.Error = staleJobReason(original)
		}
		job.UpdatedAt = now
		job.CompletedAt = now
		state.Jobs[id] = job
		changed = true
	}
	return changed
}

func staleJobReason(original sendJobStatus) string {
	if original == sendJobRunning {
		return "job marked failed after stale running timeout; processor may have exited"
	}
	return "job marked failed after stale pending timeout; processor may have exited"
}

func newSendJobID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "sj_" + strconv.FormatInt(time.Now().UnixNano(), 36) + "_" + hex.EncodeToString(b[:])
	}
	return "sj_" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func isTerminalSendJobStatus(status sendJobStatus) bool {
	return status == sendJobCompleted || status == sendJobFailed || status == sendJobCanceled
}

func canTransitionSendJobStatus(from, to sendJobStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case sendJobPending:
		return to == sendJobRunning || to == sendJobCompleted || to == sendJobFailed || to == sendJobCanceled
	case sendJobRunning:
		return to == sendJobCompleted || to == sendJobFailed
	case sendJobCompleted, sendJobFailed, sendJobCanceled:
		return false
	default:
		return false
	}
}

func waitForSessionQueueTurnForJob(store *sendJobStore, sessionName, jobID string) (*os.File, error) {
	jobID = strings.TrimSpace(jobID)
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
		time.Sleep(20 * time.Millisecond)
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
	return candidate.ID < existing.ID
}

func writeJobStatusResult(w io.Writer, gf GlobalFlags, version string, job sendJob) {
	if gf.JSON {
		PrintJSON(w, sendJobToResult(job), version)
		return
	}
	PrintHuman(w, func(w io.Writer) {
		out := sendJobToResult(job)
		if out.Error != "" {
			_, _ = io.WriteString(w, "job "+out.JobID+" "+out.Status+" ("+out.Error+")\n")
			return
		}
		_, _ = io.WriteString(w, "job "+out.JobID+" "+out.Status+"\n")
	})
}
