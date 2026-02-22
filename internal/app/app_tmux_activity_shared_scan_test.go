package app

import (
	"errors"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/tmux"
)

type sessionsWithTagsStubTmuxOps struct {
	stubTmuxOps
	rows []tmux.SessionTagValues
	err  error
}

func (s sessionsWithTagsStubTmuxOps) SessionsWithTags(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
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
