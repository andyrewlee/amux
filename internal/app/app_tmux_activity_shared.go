package app

import (
	"errors"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/tmux"
)

var errTmuxActivityOwnershipLostAfterPublish = errors.New("tmux activity ownership lost after snapshot publish")

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
) (tmuxActivityRole, map[string]bool, map[string]activity.AgentState, bool, int64, error) {
	// instanceID is assigned once at init; trim once here so all lease-owner
	// comparisons use the same normalized representation.
	instanceID := strings.TrimSpace(a.instanceID)
	lease, err := activity.ReadOwnerLease(opts)
	if err != nil {
		// Epoch 0 is intentional on unresolved ownership; callers normalize to 1
		// only when publishing as owner in a known epoch.
		return tmuxActivityRoleOwner, nil, nil, false, 0, err
	}
	if activity.OwnerLeaseAlive(lease, now) && lease.OwnerID != instanceID {
		active, states, ok, err := activity.ReadSnapshotWithStates(opts, now, lease.Epoch)
		if err != nil {
			return tmuxActivityRoleFollower, nil, nil, false, lease.Epoch, err
		}
		return tmuxActivityRoleFollower, active, states, ok, lease.Epoch, nil
	}
	if activity.OwnerLeaseAlive(lease, now) && lease.OwnerID == instanceID {
		epoch := lease.Epoch
		if epoch < 1 {
			epoch = 1
		}
		// Renew the heartbeat at scan START, not only after publish. A scan can
		// outlive the lease TTL (~7s) while the heartbeat tick (~5s) is gated
		// behind the scan; without renewing up front a long scan lets the lease
		// expire mid-scan and a follower legitimately claims it, causing
		// ownership flapping (spinner blanking, duplicate scans). The heartbeat
		// is a single tmux global-option write owned solely by this instance,
		// and publish still re-validates ownership/epoch before its own renew,
		// so refreshing here cannot create a second owner or a double-renew race.
		if err := activity.RenewOwnerLeaseHeartbeat(opts, now); err != nil {
			return tmuxActivityRoleOwner, nil, nil, false, epoch, err
		}
		return tmuxActivityRoleOwner, nil, nil, false, epoch, nil
	}

	candidateEpoch := lease.Epoch + 1
	if candidateEpoch < 1 {
		candidateEpoch = 1
	}
	// tmux global options provide no atomic compare-and-swap primitive. Claim by
	// write-then-confirm-read and rely on epoch checks to prevent split-brain use.
	if err := activity.WriteOwnerLease(opts, instanceID, candidateEpoch, now); err != nil {
		return tmuxActivityRoleOwner, nil, nil, false, candidateEpoch, err
	}
	confirmedLease, err := activity.ReadOwnerLease(opts)
	if err != nil {
		return tmuxActivityRoleOwner, nil, nil, false, candidateEpoch, err
	}
	if confirmedLease.OwnerID != instanceID || confirmedLease.Epoch != candidateEpoch {
		active, states, ok, err := activity.ReadSnapshotWithStates(opts, now, confirmedLease.Epoch)
		if err != nil {
			return tmuxActivityRoleFollower, nil, nil, false, confirmedLease.Epoch, err
		}
		return tmuxActivityRoleFollower, active, states, ok, confirmedLease.Epoch, nil
	}
	return tmuxActivityRoleOwner, nil, nil, false, candidateEpoch, nil
}

func (a *App) canPublishTmuxActivitySnapshot(opts tmux.Options, epoch int64, now time.Time) (bool, int64, error) {
	instanceID := strings.TrimSpace(a.instanceID)
	if instanceID == "" {
		return false, 0, nil
	}
	lease, err := activity.ReadOwnerLease(opts)
	if err != nil {
		return false, 0, err
	}
	if !activity.OwnerLeaseAlive(lease, now) {
		return false, lease.Epoch, nil
	}
	if lease.OwnerID != instanceID || lease.Epoch != epoch {
		return false, lease.Epoch, nil
	}
	return true, lease.Epoch, nil
}

func (a *App) publishTmuxActivitySnapshot(opts tmux.Options, active map[string]bool, states map[string]activity.AgentState, epoch int64, now time.Time) error {
	if err := tmux.SetGlobalOptionValue(activity.SnapshotOption, activity.EncodeSnapshotWithStates(active, states, epoch, now), opts); err != nil {
		return err
	}
	// Snapshot write and ownership validation are not atomic; epoch checks on
	// reads ensure followers ignore snapshots from superseded owners.
	canPublish, _, err := a.canPublishTmuxActivitySnapshot(opts, epoch, time.Now())
	if err != nil {
		return err
	}
	if !canPublish {
		return errTmuxActivityOwnershipLostAfterPublish
	}
	// Heartbeat renewal may race with ownership turnover. Ownership/epoch checks
	// on readers and subsequent scans handle this by treating mismatches as stale.
	return activity.RenewOwnerLeaseHeartbeat(opts, now)
}
