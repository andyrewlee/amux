package app

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/tmux"
)

// TestPublishTmuxActivitySnapshot_RenewsAgedSelfLease pins the publish-path
// contract the pre-publish heartbeat renewal serves: an owner whose scan
// aged its heartbeat near/past the TTL — without anyone claiming the lease —
// still publishes, keeps its owner/epoch, and leaves a fresh heartbeat so
// the next follower resolve cannot treat the lease as expired mid-publish.
//
// The ordering of the pre-publish renewal against the snapshot write is not
// state-observable through the lease alone (the heartbeat option records
// only the newest timestamp); this test pins the observable invariant.
func TestPublishTmuxActivitySnapshot_RenewsAgedSelfLease(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)

	owner := &App{instanceID: "owner-publish-renew"}
	now := time.Now()
	_, _, _, _, epoch, err := owner.resolveTmuxActivityScanRole(opts, now)
	if err != nil {
		t.Fatalf("resolve owner role: %v", err)
	}

	// Wind the heartbeat back past the TTL — the scan "ran" longer than the
	// lease — while leaving owner/epoch ours and unclaimed.
	aged := now.Add(-activity.OwnerLeaseTTL - time.Second)
	if err := activity.WriteOwnerLease(opts, owner.instanceID, epoch, aged); err != nil {
		t.Fatalf("backdate heartbeat: %v", err)
	}

	if err := owner.publishTmuxActivitySnapshot(opts, map[string]bool{"ws-a": true}, nil, epoch, now); err != nil {
		t.Fatalf("publish snapshot with aged heartbeat: %v", err)
	}

	lease, err := activity.ReadOwnerLease(opts)
	if err != nil {
		t.Fatalf("read owner lease: %v", err)
	}
	if lease.OwnerID != owner.instanceID || lease.Epoch != epoch {
		t.Fatalf("lease drifted: owner=%q epoch=%d, want %q epoch %d",
			lease.OwnerID, lease.Epoch, owner.instanceID, epoch)
	}
	if lease.HeartbeatAt.UnixMilli() <= aged.UnixMilli() {
		t.Fatalf("heartbeat not renewed by publish: %d <= aged %d",
			lease.HeartbeatAt.UnixMilli(), aged.UnixMilli())
	}
	if !activity.OwnerLeaseAlive(lease, now) {
		t.Fatal("expected lease alive at publish time after renewal")
	}
}

// TestPublishTmuxActivitySnapshot_StillRejectsSuccessorTakeover pins the
// fencing half: a lease claimed by another instance mid-scan must still fail
// the post-publish owner/epoch check even with the new pre-publish renewal.
func TestPublishTmuxActivitySnapshot_StillRejectsSuccessorTakeover(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)

	owner := &App{instanceID: "owner-pre-takeover"}
	now := time.Now()
	_, _, _, _, epoch, err := owner.resolveTmuxActivityScanRole(opts, now)
	if err != nil {
		t.Fatalf("resolve owner role: %v", err)
	}

	// A successor claims the lease between this scan's renewal and publish.
	successor := &App{instanceID: "owner-successor-takeover"}
	aged := now.Add(-activity.OwnerLeaseTTL - time.Second)
	if err := activity.WriteOwnerLease(opts, owner.instanceID, epoch, aged); err != nil {
		t.Fatalf("backdate heartbeat: %v", err)
	}
	if _, _, _, _, successorEpoch, err := successor.resolveTmuxActivityScanRole(opts, now); err != nil {
		t.Fatalf("successor claim: %v", err)
	} else if successorEpoch == epoch {
		t.Fatalf("successor must claim a new epoch, got %d", successorEpoch)
	}

	if err := owner.publishTmuxActivitySnapshot(opts, map[string]bool{"ws-a": true}, nil, epoch, now); err == nil {
		t.Fatal("publish must be rejected after a successor claimed the lease")
	}
}

// TestPublishTmuxActivitySnapshot_SnapshotOptionWritten pins that the
// publish still lands the shared snapshot followers read.
func TestPublishTmuxActivitySnapshot_SnapshotOptionWritten(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)

	owner := &App{instanceID: "owner-snapshot-write"}
	now := time.Now()
	_, _, _, _, epoch, err := owner.resolveTmuxActivityScanRole(opts, now)
	if err != nil {
		t.Fatalf("resolve owner role: %v", err)
	}

	if err := owner.publishTmuxActivitySnapshot(opts, map[string]bool{"ws-a": true}, nil, epoch, now); err != nil {
		t.Fatalf("publish snapshot: %v", err)
	}

	raw, err := tmux.GlobalOptionValue(activity.SnapshotOption, opts)
	if err != nil {
		t.Fatalf("read snapshot option: %v", err)
	}
	active, _, snapEpoch, _, ok := activity.DecodeSnapshotWithStates(raw)
	if !ok {
		t.Fatalf("published snapshot did not decode: %q", raw)
	}
	if snapEpoch != epoch {
		t.Fatalf("snapshot epoch %d, want %d", snapEpoch, epoch)
	}
	if !active["ws-a"] {
		t.Fatalf("snapshot missing ws-a, got %v", active)
	}
}
