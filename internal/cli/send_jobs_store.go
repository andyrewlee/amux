package cli

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
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
	Sequence    int64         `json:"sequence,omitempty"`
	CreatedAt   int64         `json:"created_at"`
	UpdatedAt   int64         `json:"updated_at"`
	CompletedAt int64         `json:"completed_at,omitempty"`
}

type sendJobState struct {
	Version      int                `json:"version"`
	NextSequence int64              `json:"next_sequence,omitempty"`
	Jobs         map[string]sendJob `json:"jobs"`
}

type sendJobStore struct {
	path string
}

func (s *sendJobStore) domain() sendJobDomain {
	return newSendJobDomain()
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
	domain := s.domain()
	domain.reconcileStale(state)
	domain.prune(state)

	job := domain.newJob(state, sessionName, agentID)
	state.Jobs[job.ID] = job
	if err := s.saveState(state); err != nil {
		return sendJob{}, err
	}
	return job, nil
}

func (s *sendJobStore) get(jobID string) (sendJob, bool, error) {
	// Exclusive lock: reconcileStale below may write back cleaned-up state.
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
	domain := s.domain()
	if domain.reconcileStale(state) {
		if err := s.saveState(state); err != nil {
			return sendJob{}, false, false, err
		}
	}
	job, found, canceled := domain.cancel(state, jobID)
	if !found {
		return sendJob{}, false, false, nil
	}
	if !canceled {
		return job, true, false, nil
	}
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
	domain := s.domain()
	if domain.reconcileStale(state) {
		if err := s.saveState(state); err != nil {
			return sendJob{}, err
		}
	}
	job, err := domain.setStatus(state, jobID, status, errText)
	if err != nil {
		return sendJob{}, err
	}
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
		if removeErr := os.Remove(tmpPath); removeErr != nil {
			slog.Debug("failed to remove temp file after rename failure", "path", tmpPath, "error", removeErr)
		}
		return err
	}
	return nil
}

func (s *sendJobStore) reconcileStale(state *sendJobState) bool {
	return s.domain().reconcileStale(state)
}

func newSendJobID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "sj_" + strconv.FormatInt(time.Now().UnixNano(), 36) + "_" + hex.EncodeToString(b[:])
	}
	return "sj_" + strconv.FormatInt(time.Now().UnixNano(), 36)
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
