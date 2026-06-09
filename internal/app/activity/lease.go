package activity

import (
	"encoding/base64"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// tmux global option names backing the cross-instance activity-scan lease.
const (
	OwnerOption     = "@amux_activity_owner"
	HeartbeatOption = "@amux_activity_owner_heartbeat_ms"
	EpochOption     = "@amux_activity_owner_epoch"
	SnapshotOption  = "@amux_activity_active_workspaces"
)

const (
	// OwnerLeaseTTL controls how long an activity-scan owner lease stays valid
	// after its last heartbeat before another instance may claim ownership.
	OwnerLeaseTTL = 7 * time.Second
	// OwnerFutureSkewTolerance caps how far in the future a lease heartbeat may be
	// (clock skew) before it is treated as expired rather than alive.
	OwnerFutureSkewTolerance = 2 * time.Second
	// SnapshotStaleAfter controls how long a shared activity snapshot is trusted
	// after its timestamp before followers ignore it.
	SnapshotStaleAfter = 10 * time.Second
	// SnapshotFutureSkewTolerance caps how far in the future a shared snapshot
	// timestamp may be (clock skew) before it is treated as stale rather than
	// fresh, mirroring OwnerFutureSkewTolerance for the owner lease.
	SnapshotFutureSkewTolerance = 2 * time.Second
)

// OwnerLease is the cross-instance activity-scan owner lease stored in tmux
// global options.
type OwnerLease struct {
	OwnerID     string
	HeartbeatAt time.Time
	Epoch       int64
}

// OwnerLeaseAlive reports whether lease is a live owner lease at now, tolerating
// small forward clock skew but expiring stale or far-future heartbeats.
func OwnerLeaseAlive(lease OwnerLease, now time.Time) bool {
	if strings.TrimSpace(lease.OwnerID) == "" {
		return false
	}
	if lease.HeartbeatAt.IsZero() {
		return false
	}
	if lease.HeartbeatAt.After(now) {
		return lease.HeartbeatAt.Sub(now) <= OwnerFutureSkewTolerance
	}
	return now.Sub(lease.HeartbeatAt) <= OwnerLeaseTTL
}

// ReadOwnerLease reads the current owner lease from tmux global options.
func ReadOwnerLease(opts tmux.Options) (OwnerLease, error) {
	lease := OwnerLease{}
	values, err := tmux.GlobalOptionValues([]string{
		OwnerOption,
		HeartbeatOption,
		EpochOption,
	}, opts)
	if err != nil {
		return lease, err
	}
	lease.OwnerID = strings.TrimSpace(values[OwnerOption])

	heartbeatRaw := strings.TrimSpace(values[HeartbeatOption])
	if heartbeatRaw != "" {
		heartbeatMS, parseErr := strconv.ParseInt(heartbeatRaw, 10, 64)
		if parseErr == nil && heartbeatMS > 0 {
			lease.HeartbeatAt = time.UnixMilli(heartbeatMS)
		}
	}

	epochRaw := strings.TrimSpace(values[EpochOption])
	if epochRaw != "" {
		epoch, parseErr := strconv.ParseInt(epochRaw, 10, 64)
		if parseErr == nil && epoch > 0 {
			lease.Epoch = epoch
		}
	}
	return lease, nil
}

// WriteOwnerLease claims ownership by writing owner/epoch/heartbeat. tmux global
// options offer no atomic CAS; callers confirm by re-reading and checking epoch.
func WriteOwnerLease(opts tmux.Options, ownerID string, epoch int64, now time.Time) error {
	if epoch < 1 {
		epoch = 1
	}
	return tmux.SetGlobalOptionValues([]tmux.OptionValue{
		{Key: OwnerOption, Value: strings.TrimSpace(ownerID)},
		{Key: EpochOption, Value: strconv.FormatInt(epoch, 10)},
		{Key: HeartbeatOption, Value: strconv.FormatInt(now.UnixMilli(), 10)},
	}, opts)
}

// RenewOwnerLeaseHeartbeat refreshes only the heartbeat timestamp.
func RenewOwnerLeaseHeartbeat(opts tmux.Options, now time.Time) error {
	return tmux.SetGlobalOptionValue(HeartbeatOption, strconv.FormatInt(now.UnixMilli(), 10), opts)
}

// ReadSnapshot reads the shared active-workspaces snapshot, returning ok=false
// when it is missing, malformed, from a different epoch, or stale.
func ReadSnapshot(opts tmux.Options, now time.Time, expectedEpoch int64) (map[string]bool, bool, error) {
	raw, err := tmux.GlobalOptionValue(SnapshotOption, opts)
	if err != nil {
		return nil, false, err
	}
	parsed, snapshotEpoch, at, ok := DecodeSnapshot(raw)
	if !ok {
		return nil, false, nil
	}
	if expectedEpoch > 0 && snapshotEpoch != expectedEpoch {
		return nil, false, nil
	}
	if !snapshotFresh(at, now) {
		return nil, false, nil
	}
	return parsed, true, nil
}

// snapshotFresh reports whether a snapshot timestamped at is fresh relative to
// now, tolerating only small forward clock skew. A far-future timestamp (a peer
// whose clock runs minutes/hours ahead) would otherwise pin every follower to a
// stale active set indefinitely, ignoring SnapshotStaleAfter.
func snapshotFresh(at, now time.Time) bool {
	if at.After(now) {
		return at.Sub(now) <= SnapshotFutureSkewTolerance
	}
	return now.Sub(at) <= SnapshotStaleAfter
}

// EncodeSnapshot serializes the active-workspace set with epoch and timestamp.
func EncodeSnapshot(active map[string]bool, epoch int64, now time.Time) string {
	if epoch < 1 {
		epoch = 1
	}
	ids := make([]string, 0, len(active))
	for wsID, isActive := range active {
		if isActive {
			trimmed := strings.TrimSpace(wsID)
			if trimmed != "" {
				ids = append(ids, trimmed)
			}
		}
	}
	sort.Strings(ids)
	encodedPayload, err := json.Marshal(ids)
	if err != nil {
		encodedPayload = []byte("[]")
	}
	payload := "j:" + base64.RawURLEncoding.EncodeToString(encodedPayload)
	return strconv.FormatInt(epoch, 10) + ";" + strconv.FormatInt(now.UnixMilli(), 10) + ";" + payload
}

// DecodeSnapshot parses an encoded snapshot, accepting both the JSON payload and
// legacy comma-delimited formats. Returns ok=false on malformed input.
func DecodeSnapshot(raw string) (map[string]bool, int64, time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, 0, time.Time{}, false
	}
	parts := strings.SplitN(raw, ";", 3)
	if len(parts) != 3 {
		return nil, 0, time.Time{}, false
	}
	epoch, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || epoch <= 0 {
		return nil, 0, time.Time{}, false
	}
	timestampMS, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil || timestampMS <= 0 {
		return nil, 0, time.Time{}, false
	}
	active := make(map[string]bool)
	payload := strings.TrimSpace(parts[2])
	if payload == "" {
		return active, epoch, time.UnixMilli(timestampMS), true
	}
	if strings.HasPrefix(payload, "j:") {
		if parsed, ok := decodeSnapshotJSONPayload(payload); ok {
			return parsed, epoch, time.UnixMilli(timestampMS), true
		}
	}

	legacyCandidates := make([]string, 0)
	for _, candidate := range strings.Split(payload, ",") {
		wsID := strings.TrimSpace(candidate)
		if wsID != "" {
			legacyCandidates = append(legacyCandidates, wsID)
		}
	}
	if len(legacyCandidates) == 0 {
		return active, epoch, time.UnixMilli(timestampMS), true
	}

	// Legacy payloads: comma-delimited plain IDs with optional b:<base64(id)> entries.
	// Note: plain IDs that literally start with "b:" and are valid base64 will be
	// interpreted as encoded legacy IDs by design for backward compatibility.
	for _, candidate := range legacyCandidates {
		if !strings.HasPrefix(candidate, "b:") {
			active[candidate] = true
			continue
		}

		decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(candidate, "b:"))
		if err != nil {
			active[candidate] = true
			continue
		}

		wsID := strings.TrimSpace(string(decoded))
		if wsID == "" {
			active[candidate] = true
			continue
		}
		active[wsID] = true
	}
	return active, epoch, time.UnixMilli(timestampMS), true
}

// decodeSnapshotJSONPayload decodes a "j:"-prefixed base64 JSON array of
// workspace IDs into an active set. Returns ok=false on malformed input.
func decodeSnapshotJSONPayload(payload string) (map[string]bool, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(payload, "j:"))
	if err != nil {
		return nil, false
	}
	var ids []string
	if err := json.Unmarshal(decoded, &ids); err != nil {
		return nil, false
	}
	active := make(map[string]bool)
	for _, candidate := range ids {
		wsID := strings.TrimSpace(candidate)
		if wsID == "" {
			continue
		}
		active[wsID] = true
	}
	return active, true
}
