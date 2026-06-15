package app

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/tmux"
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

func TestOwnerLeaseAlive_FutureHeartbeatTolerance(t *testing.T) {
	now := time.Now()
	lease := activity.OwnerLease{
		OwnerID: "owner-a",
	}
	lease.HeartbeatAt = now.Add(activity.OwnerFutureSkewTolerance - time.Millisecond)
	if !activity.OwnerLeaseAlive(lease, now) {
		t.Fatal("expected lease to be alive for small forward clock skew")
	}
	lease.HeartbeatAt = now.Add(activity.OwnerFutureSkewTolerance + time.Millisecond)
	if activity.OwnerLeaseAlive(lease, now) {
		t.Fatal("expected lease to be stale for large forward clock skew")
	}
}

func TestPublishTmuxActivitySnapshot_ReturnsOwnershipLostAfterPublish(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)

	now := time.Now()
	app := &App{instanceID: "owner-a"}
	if err := activity.WriteOwnerLease(opts, "owner-b", 9, now); err != nil {
		t.Fatalf("write owner lease: %v", err)
	}
	err := app.publishTmuxActivitySnapshot(opts, map[string]bool{"ws-a": true}, 9, now)
	if !errors.Is(err, errTmuxActivityOwnershipLostAfterPublish) {
		t.Fatalf("expected ownership-loss error, got %v", err)
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

	shared, ok, err := activity.ReadSnapshot(opts, now, epoch+1)
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
	if err := activity.WriteOwnerLease(opts, "other-owner", 7, now); err != nil {
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

func TestResolveTmuxActivityScanRole_OwnerResolveRenewsHeartbeatAtScanStart(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)

	owner := &App{instanceID: "owner-resolve-heartbeat"}
	now := time.Now()
	_, _, _, epoch, err := owner.resolveTmuxActivityScanRole(opts, now)
	if err != nil {
		t.Fatalf("resolve owner role: %v", err)
	}
	if err := owner.publishTmuxActivitySnapshot(opts, map[string]bool{"ws-a": true}, epoch, now); err != nil {
		t.Fatalf("publish snapshot: %v", err)
	}

	beforeRaw, err := tmux.GlobalOptionValue(activity.HeartbeatOption, opts)
	if err != nil {
		t.Fatalf("read heartbeat before resolve: %v", err)
	}
	beforeHeartbeat, err := strconv.ParseInt(beforeRaw, 10, 64)
	if err != nil {
		t.Fatalf("parse heartbeat before resolve: %v", err)
	}

	// A long scan: re-resolving 2s later (beyond the heartbeat tick) must push
	// the heartbeat forward so the lease cannot expire mid-scan.
	renewAt := now.Add(2 * time.Second)
	role, _, _, renewedEpoch, err := owner.resolveTmuxActivityScanRole(opts, renewAt)
	if err != nil {
		t.Fatalf("resolve owner role again: %v", err)
	}
	if role != tmuxActivityRoleOwner {
		t.Fatalf("expected owner role, got %v", role)
	}
	if renewedEpoch != epoch {
		t.Fatalf("expected owner epoch %d, got %d", epoch, renewedEpoch)
	}

	afterRaw, err := tmux.GlobalOptionValue(activity.HeartbeatOption, opts)
	if err != nil {
		t.Fatalf("read heartbeat after resolve: %v", err)
	}
	afterHeartbeat, err := strconv.ParseInt(afterRaw, 10, 64)
	if err != nil {
		t.Fatalf("parse heartbeat after resolve: %v", err)
	}
	if afterHeartbeat != renewAt.UnixMilli() {
		t.Fatalf("expected owner resolve to renew heartbeat to %d, got %d", renewAt.UnixMilli(), afterHeartbeat)
	}
	if afterHeartbeat <= beforeHeartbeat {
		t.Fatalf("expected owner resolve to advance heartbeat past %d, got %d", beforeHeartbeat, afterHeartbeat)
	}

	// Ownership must remain single-owner after the scan-start renew: re-reading
	// the lease shows the same owner/epoch, just a fresher heartbeat.
	lease, err := activity.ReadOwnerLease(opts)
	if err != nil {
		t.Fatalf("read owner lease: %v", err)
	}
	if lease.OwnerID != owner.instanceID {
		t.Fatalf("expected lease owner %q, got %q", owner.instanceID, lease.OwnerID)
	}
	if lease.Epoch != epoch {
		t.Fatalf("expected lease epoch %d, got %d", epoch, lease.Epoch)
	}
}

func TestEncodeDecodeTmuxActivitySnapshot_EncodesWorkspaceIDsSafely(t *testing.T) {
	now := time.Now()
	raw := activity.EncodeSnapshot(map[string]bool{
		"ws-with,comma": true,
		"ws/with space": true,
	}, 7, now)

	active, epoch, at, ok := activity.DecodeSnapshot(raw)
	if !ok {
		t.Fatalf("expected snapshot to decode, raw=%q", raw)
	}
	if epoch != 7 {
		t.Fatalf("expected epoch 7, got %d", epoch)
	}
	if at.UnixMilli() != now.UnixMilli() {
		t.Fatalf("expected timestamp %d, got %d", now.UnixMilli(), at.UnixMilli())
	}
	if !active["ws-with,comma"] || !active["ws/with space"] {
		t.Fatalf("expected decoded workspace IDs with delimiters, got %v", active)
	}
}

func TestDecodeTmuxActivitySnapshot_LegacyUnencodedWorkspaceIDs(t *testing.T) {
	raw := "3;1700000000000;ws-a,ws-b"
	active, epoch, at, ok := activity.DecodeSnapshot(raw)
	if !ok {
		t.Fatalf("expected legacy snapshot to decode, raw=%q", raw)
	}
	if epoch != 3 {
		t.Fatalf("expected epoch 3, got %d", epoch)
	}
	if at.UnixMilli() != 1700000000000 {
		t.Fatalf("expected timestamp 1700000000000, got %d", at.UnixMilli())
	}
	if !active["ws-a"] || !active["ws-b"] {
		t.Fatalf("expected legacy workspace IDs to remain readable, got %v", active)
	}
}

func TestDecodeTmuxActivitySnapshot_LegacyBEncodedWorkspaceIDs(t *testing.T) {
	raw := "3;1700000000000;b:d3MtYQ,b:d3MtYg"
	active, epoch, at, ok := activity.DecodeSnapshot(raw)
	if !ok {
		t.Fatalf("expected legacy b:-encoded snapshot to decode, raw=%q", raw)
	}
	if epoch != 3 {
		t.Fatalf("expected epoch 3, got %d", epoch)
	}
	if at.UnixMilli() != 1700000000000 {
		t.Fatalf("expected timestamp 1700000000000, got %d", at.UnixMilli())
	}
	if !active["ws-a"] || !active["ws-b"] {
		t.Fatalf("expected legacy decoded workspace IDs, got %v", active)
	}
}

func TestDecodeTmuxActivitySnapshot_LegacyMixedEncodedAndPlainWorkspaceIDs(t *testing.T) {
	raw := "3;1700000000000;b:d3MtYQ,ws-b"
	active, _, _, ok := activity.DecodeSnapshot(raw)
	if !ok {
		t.Fatalf("expected mixed legacy snapshot to decode, raw=%q", raw)
	}
	if !active["ws-a"] {
		t.Fatalf("expected legacy b:-encoded id to decode, got %v", active)
	}
	if !active["ws-b"] {
		t.Fatalf("expected legacy plain id to remain literal, got %v", active)
	}
}

func TestDecodeTmuxActivitySnapshot_LegacyPlainWorkspaceIDStartingWithJPrefix(t *testing.T) {
	raw := "3;1700000000000;j:ws-plain,ws-b"
	active, _, _, ok := activity.DecodeSnapshot(raw)
	if !ok {
		t.Fatalf("expected legacy plain snapshot to decode, raw=%q", raw)
	}
	if !active["j:ws-plain"] || !active["ws-b"] {
		t.Fatalf("expected legacy plain IDs to remain readable, got %v", active)
	}
}

func TestDecodeTmuxActivitySnapshot_LegacyBPrefixIDsRemainLiteral(t *testing.T) {
	raw := "3;1700000000000;b:workspace,ws-b"
	active, _, _, ok := activity.DecodeSnapshot(raw)
	if !ok {
		t.Fatalf("expected legacy snapshot to decode, raw=%q", raw)
	}
	if !active["b:workspace"] {
		t.Fatalf("expected legacy b:-prefixed ID to remain literal, got %v", active)
	}
	if !active["ws-b"] {
		t.Fatalf("expected additional legacy ID to decode, got %v", active)
	}
}

func TestDecodeTmuxActivitySnapshot_LegacyBPrefixValidBase64Decodes(t *testing.T) {
	raw := "3;1700000000000;b:d3M,ws-b"
	active, _, _, ok := activity.DecodeSnapshot(raw)
	if !ok {
		t.Fatalf("expected legacy snapshot to decode, raw=%q", raw)
	}
	if !active["ws"] {
		t.Fatalf("expected valid legacy b:-prefixed token to decode, got %v", active)
	}
	if active["b:d3M"] {
		t.Fatalf("expected encoded legacy token not to remain literal, got %v", active)
	}
}
