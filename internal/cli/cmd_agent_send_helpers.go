package cli

import (
	"crypto/rand"
	"encoding/hex"
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

func newAgentTabID() string {
	nowPart := strconv.FormatInt(time.Now().UnixNano(), 36)
	seqPart := strconv.FormatUint(atomic.AddUint64(&agentRunTabCounter, 1), 36)
	var entropy [4]byte
	if _, err := rand.Read(entropy[:]); err == nil {
		return "t_" + nowPart + "_" + seqPart + "_" + hex.EncodeToString(entropy[:])
	}
	return "t_" + nowPart + "_" + seqPart
}
