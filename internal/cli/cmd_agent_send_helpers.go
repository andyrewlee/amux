package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

var agentRunTabCounter uint64

func markSendJobFailedIfPresent(jobID, reason string) {
	jobID = strings.TrimSpace(jobID)
	reason = strings.TrimSpace(reason)
	if jobID == "" {
		return
	}
	jobStore, err := newSendJobStore()
	if err != nil {
		return
	}
	_, _ = jobStore.setStatus(jobID, sendJobFailed, reason)
}

func handleSendJobNotRunnable(
	w io.Writer,
	wErr io.Writer,
	gf GlobalFlags,
	version string,
	idempotencyKey string,
	sessionName string,
	agentID string,
	job sendJob,
) int {
	switch job.Status {
	case sendJobCanceled:
		result := agentSendResult{
			SessionName: sessionName,
			AgentID:     agentID,
			JobID:       job.ID,
			Status:      string(sendJobCanceled),
			Sent:        false,
		}
		if gf.JSON {
			return returnJSONSuccessWithIdempotency(
				w, wErr, gf, version, "agent.send", idempotencyKey, result,
			)
		}
		PrintHuman(w, func(w io.Writer) {
			fmt.Fprintf(w, "Send job %s canceled before execution\n", job.ID)
		})
		return ExitOK
	case sendJobCompleted:
		result := agentSendResult{
			SessionName: sessionName,
			AgentID:     agentID,
			JobID:       job.ID,
			Status:      string(sendJobCompleted),
			Sent:        true,
		}
		if gf.JSON {
			return returnJSONSuccessWithIdempotency(
				w, wErr, gf, version, "agent.send", idempotencyKey, result,
			)
		}
		PrintHuman(w, func(w io.Writer) {
			fmt.Fprintf(w, "Send job %s already completed\n", job.ID)
		})
		return ExitOK
	default:
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.send", idempotencyKey,
				ExitInternalError, "job_status_conflict", "send job is not runnable", map[string]any{
					"job_id": job.ID,
					"status": string(job.Status),
					"error":  job.Error,
				},
			)
		}
		if strings.TrimSpace(job.Error) != "" {
			Errorf(wErr, "send job %s is %s: %s", job.ID, job.Status, job.Error)
		} else {
			Errorf(wErr, "send job %s is %s and cannot be executed", job.ID, job.Status)
		}
		return ExitInternalError
	}
}

func newAgentTabID() string {
	nowPart := strconv.FormatInt(time.Now().UnixNano(), 36)
	seqPart := strconv.FormatUint(atomic.AddUint64(&agentRunTabCounter, 1), 36)
	var entropy [4]byte
	if _, err := rand.Read(entropy[:]); err == nil {
		return "t_" + nowPart + "_" + seqPart + "_" + hex.EncodeToString(entropy[:])
	}
	return "t_" + nowPart + "_" + seqPart
}
