package app

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
)

// scanRoleState models ONE shared-scan pass through the external tmux
// ownership lease (activity.ReadOwnerLease / WriteOwnerLease /
// RenewOwnerLeaseHeartbeat). The machine restarts at scanRoleStart every
// scan: what persists across scans is the lease itself plus
// tmuxActivity.scannerOwner / ownershipSet / ownerEpoch, all extended state
// at the call sites. This table is the verified spec the resolution code in
// resolveTmuxActivityScanRole / publishActivitySnapshot implements — the
// same shape ptyio's restart table takes over its numeric backoff fields.
type scanRoleState uint8

const (
	// scanRoleStart: the scan is about to read the lease.
	scanRoleStart scanRoleState = iota
	// scanRoleCandidate: no live lease and the tag is not ours — a claim at
	// epoch+1 has been written and the confirming read is pending.
	scanRoleCandidate
	// scanRoleExpiredOurs: the lease is dead but the owner tag is still
	// ours — renewing in place, keeping the epoch.
	scanRoleExpiredOurs
	// scanRoleOwner: this scan runs as the owner; the heartbeat is renewed
	// at scan start, pre-publish, and post-publish.
	scanRoleOwner
	// scanRoleFollower: a live foreign lease owns the snapshot; this scan
	// reads it at the lease's epoch and applies it.
	scanRoleFollower
	// scanRoleFollowerStale: the foreign snapshot was unreadable or from a
	// superseded epoch — SkipApply.
	scanRoleFollowerStale
	// scanRoleDemoted: this scan started as owner but lost ownership at
	// publish — the result is demoted in place to SkipApply so two
	// instances never both apply locally-scanned activity (split brain).
	scanRoleDemoted
)

func (s scanRoleState) String() string {
	switch s {
	case scanRoleCandidate:
		return "candidate"
	case scanRoleExpiredOurs:
		return "expired-ours"
	case scanRoleOwner:
		return "owner"
	case scanRoleFollower:
		return "follower"
	case scanRoleFollowerStale:
		return "follower-stale"
	case scanRoleDemoted:
		return "demoted"
	default:
		return "start"
	}
}

// scanRoleEvent is the scan-role machine's alphabet. Lease epochs and
// timestamps are extended state: evtClaim always carries epoch+1, and the
// follower/demotion reads compare epochs against the lease.
type scanRoleEvent uint8

const (
	// scanEvtReadForeignAlive: the lease is live and owned by another
	// instance (resolveTmuxActivityScanRole's first branch).
	scanEvtReadForeignAlive scanRoleEvent = iota
	// scanEvtReadOursAlive: the lease is live and ours — heartbeat renewed
	// at scan START so a long scan cannot expire it mid-flight.
	scanEvtReadOursAlive
	// scanEvtReadOursExpired: the lease is dead but the owner tag is still
	// ours — nobody claimed during the gap (a claim bumps the epoch).
	scanEvtReadOursExpired
	// scanEvtRenewInPlace: renew the dead-but-ours lease, keeping the epoch
	// (a fresh epoch would re-run the owner-transition reset every pass).
	scanEvtRenewInPlace
	// scanEvtClaim: write a claim at epoch+1 then confirm by read-back —
	// tmux global options have no compare-and-swap.
	scanEvtClaim
	// scanEvtClaimConfirmed: the confirming read matches us at the claimed
	// epoch.
	scanEvtClaimConfirmed
	// scanEvtClaimLost: the confirming read shows a foreign winner.
	scanEvtClaimLost
	// scanEvtSnapshotStale: the follower's snapshot read failed or its
	// epoch does not match the lease epoch.
	scanEvtSnapshotStale
	// scanEvtPublishOwnershipLost: the publish-time revalidation failed, or
	// ownership was lost after the snapshot write (epoch mismatch) — demote
	// in place, SkipApply.
	scanEvtPublishOwnershipLost
	// scanEvtPublishConfirmed: the snapshot published under a revalidated
	// lease with the post-publish heartbeat renewed.
	scanEvtPublishConfirmed
	// scanEvtScanStart: the next scan begins (the machine restarts).
	scanEvtScanStart
)

func (e scanRoleEvent) String() string {
	switch e {
	case scanEvtReadForeignAlive:
		return "read-foreign-alive"
	case scanEvtReadOursAlive:
		return "read-ours-alive"
	case scanEvtReadOursExpired:
		return "read-ours-expired"
	case scanEvtRenewInPlace:
		return "renew-in-place"
	case scanEvtClaim:
		return "claim"
	case scanEvtClaimConfirmed:
		return "claim-confirmed"
	case scanEvtClaimLost:
		return "claim-lost"
	case scanEvtSnapshotStale:
		return "snapshot-stale"
	case scanEvtPublishOwnershipLost:
		return "publish-ownership-lost"
	case scanEvtPublishConfirmed:
		return "publish-confirmed"
	default:
		return "scan-start"
	}
}

// scanRoleMachine is the per-scan ownership pipeline. Code-site mapping:
// the read edges are resolveTmuxActivityScanRole's branch ladder (expired
// tag-still-ours → renew-in-place keeps the epoch; anything else dead or
// foreign-expired → claim at epoch+1, write-then-confirm), snapshotStale is
// runFollowerScan's !applyShared path, and the publish edges are
// publishActivitySnapshot's revalidation + post-write ownership check.
var scanRoleMachine = fsm.MustCompile(fsm.Machine[scanRoleState, scanRoleEvent]{
	Name:    "tmux-shared-scan-role",
	Initial: scanRoleStart,
	Steps: []fsm.Step[scanRoleState, scanRoleEvent]{
		{From: scanRoleStart, Event: scanEvtReadForeignAlive, To: scanRoleFollower},
		{From: scanRoleStart, Event: scanEvtReadOursAlive, To: scanRoleOwner},
		{From: scanRoleStart, Event: scanEvtReadOursExpired, To: scanRoleExpiredOurs},
		{From: scanRoleStart, Event: scanEvtClaim, To: scanRoleCandidate},
		{From: scanRoleExpiredOurs, Event: scanEvtRenewInPlace, To: scanRoleOwner},
		{From: scanRoleCandidate, Event: scanEvtClaimConfirmed, To: scanRoleOwner},
		{From: scanRoleCandidate, Event: scanEvtClaimLost, To: scanRoleFollower},
		{From: scanRoleFollower, Event: scanEvtSnapshotStale, To: scanRoleFollowerStale},
		{From: scanRoleOwner, Event: scanEvtPublishConfirmed, To: scanRoleOwner},
		{From: scanRoleOwner, Event: scanEvtPublishOwnershipLost, To: scanRoleDemoted},
		// The next scan restarts the machine from any outcome.
		{From: scanRoleFollower, Event: scanEvtScanStart, To: scanRoleStart},
		{From: scanRoleFollowerStale, Event: scanEvtScanStart, To: scanRoleStart},
		{From: scanRoleOwner, Event: scanEvtScanStart, To: scanRoleStart},
		{From: scanRoleDemoted, Event: scanEvtScanStart, To: scanRoleStart},
	},
})

// scanRoleCheck formally verifies the ownership pipeline: only an owner
// (possibly one that just renewed an expired-but-ours lease in place)
// reaches the publish edges — a follower, a stale follower, or a demoted
// ex-owner can never publish; demotion is one-way for the rest of the scan
// (the only edge out of demoted is the next scan's start — there is no
// path that un-demotes and applies in the same pass); and a claim must be
// confirmed or lost before the scan proceeds (candidate has no publish or
// apply edges).
func scanRoleCheck() error {
	if err := scanRoleMachine.Check(fsm.Checker[scanRoleState, scanRoleEvent]{
		Alphabet: []scanRoleEvent{
			scanEvtReadForeignAlive, scanEvtReadOursAlive, scanEvtReadOursExpired,
			scanEvtRenewInPlace, scanEvtClaim, scanEvtClaimConfirmed, scanEvtClaimLost,
			scanEvtSnapshotStale, scanEvtPublishOwnershipLost, scanEvtPublishConfirmed,
			scanEvtScanStart,
		},
		AllDeclaredReachable: true,
		Edge: func(from scanRoleState, event scanRoleEvent, to scanRoleState) error {
			if (event == scanEvtPublishConfirmed || event == scanEvtPublishOwnershipLost) && from != scanRoleOwner {
				return fmt.Errorf("only the owner may publish (edge %s --%s--> %s)", from, event, to)
			}
			if from == scanRoleDemoted && event != scanEvtScanStart {
				return fmt.Errorf("demotion must be one-way for the scan (illegal %s from demoted)", event)
			}
			if from == scanRoleCandidate && event != scanEvtClaimConfirmed && event != scanEvtClaimLost {
				return fmt.Errorf("a candidate must confirm or lose before anything else (illegal %s)", event)
			}
			if event == scanEvtRenewInPlace && to != scanRoleOwner {
				return errors.New("renewing an expired-but-ours lease must keep the instance owner")
			}
			return nil
		},
	}); err != nil {
		return err
	}
	// Liveness of the claim path: from candidate the scan can always still
	// finish as a resolved role (confirmed owner or lost-to-follower) — the
	// write-then-confirm dance can never wedge a scan.
	stillResolvable := scanRoleMachine.ReachableFrom(scanRoleCandidate, nil)
	for _, resolved := range []scanRoleState{scanRoleOwner, scanRoleFollower} {
		found := false
		for _, s := range stillResolvable {
			if s == resolved {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("scan role: candidate cannot reach %s — the claim path dead-ends", resolved)
		}
	}
	return nil
}
