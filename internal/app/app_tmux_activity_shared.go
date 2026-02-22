package app

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

const (
	tmuxActivityOwnerOption     = "@amux_activity_owner"
	tmuxActivityHeartbeatOption = "@amux_activity_owner_heartbeat_ms"
	tmuxActivityEpochOption     = "@amux_activity_owner_epoch"
	tmuxActivitySnapshotOption  = "@amux_activity_active_workspaces"
)

type tmuxActivityRole int

const (
	tmuxActivityRoleOwner tmuxActivityRole = iota
	tmuxActivityRoleFollower
)

func (a *App) sharedTmuxActivityEnabled() bool {
	return strings.TrimSpace(a.instanceID) != ""
}

func (a *App) resolveTmuxActivityScanRole(
	opts tmux.Options,
	now time.Time,
) (tmuxActivityRole, map[string]bool, bool, int64, error) {
	lease, err := readTmuxActivityOwnerLease(opts)
	if err != nil {
		return tmuxActivityRoleOwner, nil, false, 0, err
	}
	if ownerLeaseAlive(lease, now) && lease.ownerID != a.instanceID {
		active, ok, err := readTmuxActivitySnapshot(opts, now, lease.epoch)
		if err != nil {
			return tmuxActivityRoleFollower, nil, false, lease.epoch, err
		}
		return tmuxActivityRoleFollower, active, ok, lease.epoch, nil
	}
	if ownerLeaseAlive(lease, now) && lease.ownerID == a.instanceID {
		epoch := lease.epoch
		if epoch < 1 {
			epoch = 1
		}
		return tmuxActivityRoleOwner, nil, false, epoch, nil
	}

	candidateEpoch := lease.epoch + 1
	if candidateEpoch < 1 {
		candidateEpoch = 1
	}
	if err := writeTmuxActivityOwnerLease(opts, a.instanceID, candidateEpoch, now); err != nil {
		return tmuxActivityRoleOwner, nil, false, candidateEpoch, err
	}
	confirmedLease, err := readTmuxActivityOwnerLease(opts)
	if err != nil {
		return tmuxActivityRoleOwner, nil, false, candidateEpoch, err
	}
	if strings.TrimSpace(confirmedLease.ownerID) != a.instanceID || confirmedLease.epoch != candidateEpoch {
		active, ok, err := readTmuxActivitySnapshot(opts, now, confirmedLease.epoch)
		if err != nil {
			return tmuxActivityRoleFollower, nil, false, confirmedLease.epoch, err
		}
		return tmuxActivityRoleFollower, active, ok, confirmedLease.epoch, nil
	}
	return tmuxActivityRoleOwner, nil, false, candidateEpoch, nil
}

func (a *App) publishTmuxActivitySnapshot(opts tmux.Options, active map[string]bool, epoch int64, now time.Time) error {
	if err := tmux.SetGlobalOptionValue(tmuxActivitySnapshotOption, encodeTmuxActivitySnapshot(active, epoch, now), opts); err != nil {
		return err
	}
	return writeTmuxActivityOwnerLease(opts, a.instanceID, epoch, now)
}

type tmuxActivityLease struct {
	ownerID     string
	heartbeatAt time.Time
	epoch       int64
}

func ownerLeaseAlive(lease tmuxActivityLease, now time.Time) bool {
	if strings.TrimSpace(lease.ownerID) == "" {
		return false
	}
	if lease.heartbeatAt.IsZero() {
		return false
	}
	if lease.heartbeatAt.After(now) {
		return true
	}
	return now.Sub(lease.heartbeatAt) <= tmuxActivityOwnerLeaseTTL
}

func readTmuxActivityOwnerLease(opts tmux.Options) (tmuxActivityLease, error) {
	lease := tmuxActivityLease{}
	ownerID, err := tmux.GlobalOptionValue(tmuxActivityOwnerOption, opts)
	if err != nil {
		return lease, err
	}
	lease.ownerID = strings.TrimSpace(ownerID)

	heartbeatRaw, err := tmux.GlobalOptionValue(tmuxActivityHeartbeatOption, opts)
	if err != nil {
		return lease, err
	}
	heartbeatRaw = strings.TrimSpace(heartbeatRaw)
	if heartbeatRaw != "" {
		heartbeatMS, parseErr := strconv.ParseInt(heartbeatRaw, 10, 64)
		if parseErr == nil && heartbeatMS > 0 {
			lease.heartbeatAt = time.UnixMilli(heartbeatMS)
		}
	}

	epochRaw, err := tmux.GlobalOptionValue(tmuxActivityEpochOption, opts)
	if err != nil {
		return lease, err
	}
	epochRaw = strings.TrimSpace(epochRaw)
	if epochRaw != "" {
		epoch, parseErr := strconv.ParseInt(epochRaw, 10, 64)
		if parseErr == nil && epoch > 0 {
			lease.epoch = epoch
		}
	}
	return lease, nil
}

func writeTmuxActivityOwnerLease(opts tmux.Options, ownerID string, epoch int64, now time.Time) error {
	if epoch < 1 {
		epoch = 1
	}
	if err := tmux.SetGlobalOptionValue(tmuxActivityOwnerOption, strings.TrimSpace(ownerID), opts); err != nil {
		return err
	}
	if err := tmux.SetGlobalOptionValue(tmuxActivityEpochOption, strconv.FormatInt(epoch, 10), opts); err != nil {
		return err
	}
	return tmux.SetGlobalOptionValue(tmuxActivityHeartbeatOption, strconv.FormatInt(now.UnixMilli(), 10), opts)
}

func readTmuxActivitySnapshot(opts tmux.Options, now time.Time, expectedEpoch int64) (map[string]bool, bool, error) {
	raw, err := tmux.GlobalOptionValue(tmuxActivitySnapshotOption, opts)
	if err != nil {
		return nil, false, err
	}
	parsed, snapshotEpoch, at, ok := decodeTmuxActivitySnapshot(raw)
	if !ok {
		return nil, false, nil
	}
	if expectedEpoch > 0 && snapshotEpoch != expectedEpoch {
		return nil, false, nil
	}
	if at.After(now) {
		return parsed, true, nil
	}
	if now.Sub(at) > tmuxActivitySnapshotStaleAfter {
		return nil, false, nil
	}
	return parsed, true, nil
}

func encodeTmuxActivitySnapshot(active map[string]bool, epoch int64, now time.Time) string {
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
	return strconv.FormatInt(epoch, 10) + ";" + strconv.FormatInt(now.UnixMilli(), 10) + ";" + strings.Join(ids, ",")
}

func decodeTmuxActivitySnapshot(raw string) (map[string]bool, int64, time.Time, bool) {
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
	for _, candidate := range strings.Split(payload, ",") {
		wsID := strings.TrimSpace(candidate)
		if wsID == "" {
			continue
		}
		active[wsID] = true
	}
	return active, epoch, time.UnixMilli(timestampMS), true
}
