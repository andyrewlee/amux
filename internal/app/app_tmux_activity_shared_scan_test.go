package app

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
)

type sessionsWithTagsStubTmuxOps struct {
	stubTmuxOps
	rows           []tmux.SessionTagValues
	err            error
	onSessionsCall func()
}

func (s sessionsWithTagsStubTmuxOps) SessionsWithTags(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
	if s.onSessionsCall != nil {
		s.onSessionsCall()
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.rows, nil
}

func TestRunTmuxActivityScan_FollowerReconcilesStoppedTabsFromSharedSnapshot(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)

	now := time.Now()
	if err := writeTmuxActivityOwnerLease(opts, "shared-owner", 3, now); err != nil {
		t.Fatalf("write owner lease: %v", err)
	}
	if err := tmux.SetGlobalOptionValue(tmuxActivitySnapshotOption, encodeTmuxActivitySnapshot(map[string]bool{"ws-shared": true}, 3, now), opts); err != nil {
		t.Fatalf("set shared snapshot: %v", err)
	}

	app := &App{
		instanceID: "shared-follower",
		tmuxService: newTmuxService(sessionsWithTagsStubTmuxOps{
			stubTmuxOps: stubTmuxOps{
				allStates: map[string]tmux.SessionState{},
			},
			rows: []tmux.SessionTagValues{{
				Name: "session-a",
				Tags: map[string]string{},
			}},
		}),
	}

	infoBySession := map[string]activity.SessionInfo{
		"session-a": {
			Status:      "running",
			WorkspaceID: "ws-local",
		},
	}

	result := app.runTmuxActivityScan(1, infoBySession, map[string]*activity.SessionState{}, opts, app.tmuxService)
	if result.Err != nil {
		t.Fatalf("unexpected scan error: %v", result.Err)
	}
	if !result.RoleKnown {
		t.Fatal("expected shared role metadata")
	}
	if result.ScannerOwner {
		t.Fatal("expected follower role")
	}
	if result.ScannerEpoch != 3 {
		t.Fatalf("expected shared epoch 3, got %d", result.ScannerEpoch)
	}
	if !result.ActiveWorkspaceIDs["ws-shared"] {
		t.Fatalf("expected shared snapshot activity to be applied, got %v", result.ActiveWorkspaceIDs)
	}
	if len(result.StoppedTabs) != 1 {
		t.Fatalf("expected one stopped tab update, got %d", len(result.StoppedTabs))
	}
	if result.StoppedTabs[0].WorkspaceID != "ws-local" || result.StoppedTabs[0].SessionName != "session-a" || result.StoppedTabs[0].Status != "stopped" {
		t.Fatalf("unexpected stopped tab update: %+v", result.StoppedTabs[0])
	}
}

func TestRunTmuxActivityScan_ScanErrorIncludesResolvedOwnerMetadata(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)

	scanErr := errors.New("fetch tagged sessions failed")
	app := &App{
		instanceID: "shared-owner",
		tmuxService: newTmuxService(sessionsWithTagsStubTmuxOps{
			err: scanErr,
		}),
	}

	result := app.runTmuxActivityScan(1, map[string]activity.SessionInfo{}, map[string]*activity.SessionState{}, opts, app.tmuxService)
	if !errors.Is(result.Err, scanErr) {
		t.Fatalf("expected scan error %v, got %v", scanErr, result.Err)
	}
	if !result.RoleKnown {
		t.Fatal("expected resolved role metadata on error")
	}
	if !result.ScannerOwner {
		t.Fatal("expected owner metadata on error")
	}
	if result.ScannerEpoch < 1 {
		t.Fatalf("expected owner epoch >= 1, got %d", result.ScannerEpoch)
	}
}

func TestRunTmuxActivityScan_OwnerLeaseRevalidatedBeforePublish(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)

	initialNow := time.Now()
	if err := writeTmuxActivityOwnerLease(opts, "owner-a", 2, initialNow); err != nil {
		t.Fatalf("write initial owner lease: %v", err)
	}
	if err := tmux.SetGlobalOptionValue(tmuxActivitySnapshotOption, encodeTmuxActivitySnapshot(map[string]bool{"ws-old": true}, 2, initialNow), opts); err != nil {
		t.Fatalf("write initial snapshot: %v", err)
	}

	app := &App{
		instanceID: "owner-a",
		tmuxService: newTmuxService(sessionsWithTagsStubTmuxOps{
			stubTmuxOps: stubTmuxOps{
				allStates: map[string]tmux.SessionState{},
			},
			onSessionsCall: func() {
				if err := writeTmuxActivityOwnerLease(opts, "owner-b", 3, time.Now()); err != nil {
					t.Fatalf("simulate owner takeover: %v", err)
				}
				if err := tmux.SetGlobalOptionValue(tmuxActivitySnapshotOption, encodeTmuxActivitySnapshot(map[string]bool{"ws-new": true}, 3, time.Now()), opts); err != nil {
					t.Fatalf("write takeover snapshot: %v", err)
				}
			},
		}),
	}

	result := app.runTmuxActivityScan(42, map[string]activity.SessionInfo{}, map[string]*activity.SessionState{}, opts, app.tmuxService)
	if result.Err != nil {
		t.Fatalf("unexpected scan error: %v", result.Err)
	}
	if result.ScannerOwner {
		t.Fatal("expected scanner to drop owner flag after lease takeover")
	}
	if !result.SkipApply {
		t.Fatal("expected local apply to be skipped after lease takeover")
	}
	if result.ScannerEpoch != 3 {
		t.Fatalf("expected scanner epoch to update to current owner epoch 3, got %d", result.ScannerEpoch)
	}

	lease, err := readTmuxActivityOwnerLease(opts)
	if err != nil {
		t.Fatalf("read lease after scan: %v", err)
	}
	if lease.ownerID != "owner-b" {
		t.Fatalf("expected takeover owner to remain owner-b, got %q", lease.ownerID)
	}
	if lease.epoch != 3 {
		t.Fatalf("expected takeover epoch to remain 3, got %d", lease.epoch)
	}

	shared, ok, err := readTmuxActivitySnapshot(opts, time.Now(), 3)
	if err != nil {
		t.Fatalf("read snapshot after scan: %v", err)
	}
	if !ok {
		t.Fatal("expected takeover snapshot to remain readable for epoch 3")
	}
	if !shared["ws-new"] {
		t.Fatalf("expected takeover snapshot to stay intact, got %v", shared)
	}
}

func TestRunTmuxActivityScan_LeaseRevalidationErrorBeforePublishSkipsApply(t *testing.T) {
	skipIfNoTmux(t)
	opts := gcTestServer(t)
	if err := writeTmuxActivityOwnerLease(opts, "owner-a", 4, time.Now()); err != nil {
		t.Fatalf("write owner lease: %v", err)
	}

	app := &App{
		instanceID: "owner-a",
		tmuxService: newTmuxService(sessionsWithTagsStubTmuxOps{
			stubTmuxOps: stubTmuxOps{
				allStates: map[string]tmux.SessionState{},
			},
			rows: []tmux.SessionTagValues{},
			onSessionsCall: func() {
				cmd := exec.Command("tmux", gcTmuxArgs(opts, "kill-server")...)
				_ = cmd.Run()
			},
		}),
	}

	result := app.runTmuxActivityScan(99, map[string]activity.SessionInfo{}, map[string]*activity.SessionState{}, opts, app.tmuxService)
	if result.Err != nil {
		t.Fatalf("unexpected scan error: %v", result.Err)
	}
	if result.ScannerOwner {
		t.Fatal("expected scanner owner=false when pre-publish lease revalidation errors")
	}
	if !result.SkipApply {
		t.Fatal("expected SkipApply=true when pre-publish lease revalidation errors")
	}
}

func TestRunTmuxActivityScan_OwnerResolutionErrorLeavesRoleUnknown(t *testing.T) {
	skipIfNoTmux(t)
	opts := tmux.Options{
		ServerName:     fmt.Sprintf("amux-noserver-%d", time.Now().UnixNano()),
		ConfigPath:     "/dev/null",
		CommandTimeout: 5 * time.Second,
	}

	app := &App{
		instanceID: "owner-a",
		tmuxService: newTmuxService(sessionsWithTagsStubTmuxOps{
			stubTmuxOps: stubTmuxOps{
				allStates: map[string]tmux.SessionState{},
			},
			rows: []tmux.SessionTagValues{},
		}),
	}

	result := app.runTmuxActivityScan(11, map[string]activity.SessionInfo{}, map[string]*activity.SessionState{}, opts, app.tmuxService)
	if result.Err != nil {
		t.Fatalf("unexpected scan error: %v", result.Err)
	}
	if result.RoleKnown {
		t.Fatal("expected role to remain unknown when ownership resolution fails")
	}
	if result.ScannerEpoch != 0 {
		t.Fatalf("expected unknown-role scan epoch 0, got %d", result.ScannerEpoch)
	}
}

func TestHandleTmuxActivityResult_AppliesStoppedTabsWhenSkipApply(t *testing.T) {
	app := &App{
		tmuxActivityToken:        7,
		tmuxActivityScanInFlight: true,
	}

	expected := messages.TabSessionStatus{
		WorkspaceID: "ws-local",
		SessionName: "session-a",
		Status:      "stopped",
	}
	cmds := app.handleTmuxActivityResult(tmuxActivityResult{
		Token:       7,
		SkipApply:   true,
		StoppedTabs: []messages.TabSessionStatus{expected},
	})
	if len(cmds) != 1 {
		t.Fatalf("expected stopped-tab command to be enqueued, got %d cmds", len(cmds))
	}
	if app.tmuxActivityScanInFlight {
		t.Fatal("expected scan in-flight flag to be cleared")
	}

	msg := cmds[0]()
	got, ok := msg.(messages.TabSessionStatus)
	if !ok {
		t.Fatalf("expected TabSessionStatus command message, got %T", msg)
	}
	if got != expected {
		t.Fatalf("expected stopped-tab message %+v, got %+v", expected, got)
	}
}

func TestHandleTmuxActivityResult_AppliesStoppedTabsWhenErrSet(t *testing.T) {
	app := &App{
		tmuxActivityToken:        8,
		tmuxActivityScanInFlight: true,
	}

	expected := messages.TabSessionStatus{
		WorkspaceID: "ws-local",
		SessionName: "session-b",
		Status:      "stopped",
	}
	cmds := app.handleTmuxActivityResult(tmuxActivityResult{
		Token:       8,
		Err:         errors.New("scan failed"),
		StoppedTabs: []messages.TabSessionStatus{expected},
	})
	if len(cmds) != 1 {
		t.Fatalf("expected stopped-tab command to be enqueued despite scan error, got %d cmds", len(cmds))
	}
	if app.tmuxActivityScanInFlight {
		t.Fatal("expected scan in-flight flag to be cleared")
	}
	msg := cmds[0]()
	got, ok := msg.(messages.TabSessionStatus)
	if !ok {
		t.Fatalf("expected TabSessionStatus command message, got %T", msg)
	}
	if got != expected {
		t.Fatalf("expected stopped-tab message %+v, got %+v", expected, got)
	}
}
