package cli

import (
	"errors"
	"strings"
	"time"
)

type sendJobDomain struct {
	now        func() int64
	retention  time.Duration
	staleAfter time.Duration
}

func newSendJobDomain() sendJobDomain {
	return sendJobDomain{
		now: func() int64 {
			return time.Now().Unix()
		},
		retention:  sendJobsRetention,
		staleAfter: sendJobsStaleAfter,
	}
}

func (d sendJobDomain) newJob(state *sendJobState, sessionName, agentID string) sendJob {
	now := d.now()
	return sendJob{
		ID:          newSendJobID(),
		Command:     "agent.send",
		SessionName: sessionName,
		AgentID:     agentID,
		Status:      sendJobPending,
		Sequence:    nextSendJobSequence(state),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func (d sendJobDomain) cancel(state *sendJobState, jobID string) (sendJob, bool, bool) {
	job, ok := state.Jobs[jobID]
	if !ok {
		return sendJob{}, false, false
	}
	if job.Status != sendJobPending {
		return job, true, false
	}

	job.Status = sendJobCanceled
	job.UpdatedAt = d.now()
	job.CompletedAt = job.UpdatedAt
	state.Jobs[jobID] = job
	return job, true, true
}

func (d sendJobDomain) setStatus(
	state *sendJobState,
	jobID string,
	status sendJobStatus,
	errText string,
) (sendJob, error) {
	job, ok := state.Jobs[jobID]
	if !ok {
		return sendJob{}, errors.New("job not found")
	}
	if !canTransitionSendJobStatus(job.Status, status) {
		return job, nil
	}

	job.Status = status
	job.Error = strings.TrimSpace(errText)
	job.UpdatedAt = d.now()
	if isTerminalSendJobStatus(status) {
		job.CompletedAt = job.UpdatedAt
	}
	state.Jobs[jobID] = job
	return job, nil
}

func (d sendJobDomain) prune(state *sendJobState) {
	if state == nil || len(state.Jobs) == 0 {
		return
	}
	cutoff := d.now() - int64(d.retention/time.Second)
	for id, job := range state.Jobs {
		if job.Status == sendJobPending || job.Status == sendJobRunning {
			continue
		}
		if job.UpdatedAt <= cutoff {
			delete(state.Jobs, id)
		}
	}
}

func (d sendJobDomain) reconcileStale(state *sendJobState) bool {
	if state == nil || len(state.Jobs) == 0 {
		return false
	}
	now := d.now()
	staleCutoff := now - int64(d.staleAfter/time.Second)
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
