package app

import (
	"testing"
	"time"
)

func TestResolveTmuxActivityScanRole_OwnerFollowerSnapshotEpoch(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)

	owner := &App{instanceID: "owner-instance"}
	now := time.Now()

	role, shared, applyShared, epoch, err := owner.resolveTmuxActivityScanRole(opts, now)
	if err != nil {
		t.Fatalf("resolve owner role: %v", err)
	}
	if role != tmuxActivityRoleOwner {
		t.Fatalf("expected owner role, got %v", role)
	}
	if applyShared {
		t.Fatal("expected owner path not to apply shared snapshot")
	}
	if len(shared) != 0 {
		t.Fatalf("expected no shared snapshot for owner path, got %v", shared)
	}
	if epoch < 1 {
		t.Fatalf("expected epoch >= 1, got %d", epoch)
	}

	active := map[string]bool{"ws-a": true, "ws-b": true}
	if err := owner.publishTmuxActivitySnapshot(opts, active, epoch, now); err != nil {
		t.Fatalf("publish snapshot: %v", err)
	}

	follower := &App{instanceID: "follower-instance"}
	role, shared, applyShared, followerEpoch, err := follower.resolveTmuxActivityScanRole(opts, now.Add(500*time.Millisecond))
	if err != nil {
		t.Fatalf("resolve follower role: %v", err)
	}
	if role != tmuxActivityRoleFollower {
		t.Fatalf("expected follower role, got %v", role)
	}
	if !applyShared {
		t.Fatal("expected follower to apply shared snapshot")
	}
	if followerEpoch != epoch {
		t.Fatalf("expected follower epoch %d, got %d", epoch, followerEpoch)
	}
	if !shared["ws-a"] || !shared["ws-b"] {
		t.Fatalf("expected shared active snapshot, got %v", shared)
	}

	role, _, _, renewedEpoch, err := owner.resolveTmuxActivityScanRole(opts, now.Add(time.Second))
	if err != nil {
		t.Fatalf("resolve owner renew: %v", err)
	}
	if role != tmuxActivityRoleOwner {
		t.Fatalf("expected owner role on renew, got %v", role)
	}
	if renewedEpoch != epoch {
		t.Fatalf("expected owner renew to keep epoch %d, got %d", epoch, renewedEpoch)
	}
}

func TestReadTmuxActivitySnapshot_EpochMismatchReturnsNotOK(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)

	owner := &App{instanceID: "owner-epoch"}
	now := time.Now()
	_, _, _, epoch, err := owner.resolveTmuxActivityScanRole(opts, now)
	if err != nil {
		t.Fatalf("resolve owner role: %v", err)
	}
	if err := owner.publishTmuxActivitySnapshot(opts, map[string]bool{"ws-a": true}, epoch, now); err != nil {
		t.Fatalf("publish snapshot: %v", err)
	}

	shared, ok, err := readTmuxActivitySnapshot(opts, now, epoch+1)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if ok {
		t.Fatalf("expected epoch-mismatched snapshot to be ignored, got %v", shared)
	}
}

func TestResolveTmuxActivityScanRole_FollowerWithoutSnapshotSkipsApply(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)

	now := time.Now()
	if err := writeTmuxActivityOwnerLease(opts, "other-owner", 7, now); err != nil {
		t.Fatalf("write owner lease: %v", err)
	}

	app := &App{instanceID: "follower-only"}
	role, shared, applyShared, epoch, err := app.resolveTmuxActivityScanRole(opts, now.Add(200*time.Millisecond))
	if err != nil {
		t.Fatalf("resolve role: %v", err)
	}
	if role != tmuxActivityRoleFollower {
		t.Fatalf("expected follower role, got %v", role)
	}
	if applyShared {
		t.Fatalf("expected follower to skip apply when snapshot missing, got shared=%v", shared)
	}
	if epoch != 7 {
		t.Fatalf("expected follower epoch 7, got %d", epoch)
	}
}
