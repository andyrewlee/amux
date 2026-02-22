package app

import (
	"strconv"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

type detachedGCOps struct {
	tmuxOps

	rows      []tmux.SessionTagValues
	allStates map[string]tmux.SessionState
	clients   map[string]bool
	createdAt map[string]int64
	killed    []string
}

func (d *detachedGCOps) SessionsWithTags(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
	return d.rows, nil
}

func (d *detachedGCOps) AllSessionStates(tmux.Options) (map[string]tmux.SessionState, error) {
	if d.allStates == nil {
		return map[string]tmux.SessionState{}, nil
	}
	return d.allStates, nil
}

func (d *detachedGCOps) SessionHasClients(sessionName string, opts tmux.Options) (bool, error) {
	return d.clients[sessionName], nil
}

func (d *detachedGCOps) SessionCreatedAt(sessionName string, opts tmux.Options) (int64, error) {
	return d.createdAt[sessionName], nil
}

func (d *detachedGCOps) KillSession(sessionName string, opts tmux.Options) error {
	d.killed = append(d.killed, sessionName)
	return nil
}

func TestGcStaleDetachedAgentSessions_SkipsWhenFollower(t *testing.T) {
	app := &App{
		tmuxAvailable:            true,
		instanceID:               "instance-a",
		tmuxActivityOwnershipSet: true,
		tmuxActivityScannerOwner: false,
	}
	if cmd := app.gcStaleDetachedAgentSessions(); cmd != nil {
		t.Fatal("expected nil cmd for follower instance")
	}
}

func TestGcStaleDetachedAgentSessions_RunsWhenOwnershipUnknown(t *testing.T) {
	app := &App{
		tmuxAvailable: true,
		instanceID:    "instance-a",
	}
	if cmd := app.gcStaleDetachedAgentSessions(); cmd == nil {
		t.Fatal("expected cmd when ownership is unknown")
	}
}

func TestGcStaleDetachedAgentSessions_KillsStaleDetachedNoLivePane(t *testing.T) {
	now := time.Now()
	stale := now.Add(-(detachedAgentStaleAfter + time.Hour)).UnixMilli()

	ops := &detachedGCOps{
		rows: []tmux.SessionTagValues{
			{
				Name: "stale-agent",
				Tags: map[string]string{
					tmux.TagSessionLeaseAt: strconv.FormatInt(stale, 10),
				},
			},
		},
		allStates: map[string]tmux.SessionState{
			"stale-agent": {Exists: true, HasLivePane: false},
		},
		clients: map[string]bool{
			"stale-agent": false,
		},
	}

	app := &App{
		tmuxAvailable: true,
		tmuxService:   newTmuxService(ops),
	}
	cmd := app.gcStaleDetachedAgentSessions()
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg := cmd()
	result, ok := msg.(staleDetachedAgentGCResult)
	if !ok {
		t.Fatalf("expected staleDetachedAgentGCResult, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("unexpected GC error: %v", result.Err)
	}
	if result.Killed != 1 {
		t.Fatalf("expected killed=1, got %d", result.Killed)
	}
	if len(ops.killed) != 1 || ops.killed[0] != "stale-agent" {
		t.Fatalf("expected stale-agent to be killed, got %v", ops.killed)
	}
}

func TestGcStaleDetachedAgentSessions_RespectsLivePaneThreshold(t *testing.T) {
	now := time.Now()
	staleButNotLivePaneStale := now.Add(-(detachedAgentStaleAfter + 2*time.Hour)).UnixMilli()

	ops := &detachedGCOps{
		rows: []tmux.SessionTagValues{
			{
				Name: "live-pane-agent",
				Tags: map[string]string{
					tmux.TagSessionLeaseAt: strconv.FormatInt(staleButNotLivePaneStale, 10),
				},
			},
		},
		allStates: map[string]tmux.SessionState{
			"live-pane-agent": {Exists: true, HasLivePane: true},
		},
		clients: map[string]bool{
			"live-pane-agent": false,
		},
	}

	app := &App{
		tmuxAvailable: true,
		tmuxService:   newTmuxService(ops),
	}
	msg := app.gcStaleDetachedAgentSessions()()
	result, ok := msg.(staleDetachedAgentGCResult)
	if !ok {
		t.Fatalf("expected staleDetachedAgentGCResult, got %T", msg)
	}
	if result.Killed != 0 {
		t.Fatalf("expected killed=0 for live pane grace period, got %d", result.Killed)
	}
	if result.SkippedLivePane != 1 {
		t.Fatalf("expected skipped_live_pane=1, got %d", result.SkippedLivePane)
	}
}

func TestGcStaleDetachedAgentSessions_SkipsFreshAndAttached(t *testing.T) {
	now := time.Now()
	fresh := now.Add(-2 * time.Hour).UnixMilli()
	stale := now.Add(-48 * time.Hour).UnixMilli()

	ops := &detachedGCOps{
		rows: []tmux.SessionTagValues{
			{
				Name: "fresh-agent",
				Tags: map[string]string{
					tmux.TagSessionLeaseAt: strconv.FormatInt(fresh, 10),
				},
			},
			{
				Name: "attached-agent",
				Tags: map[string]string{
					tmux.TagSessionLeaseAt: strconv.FormatInt(stale, 10),
				},
			},
		},
		allStates: map[string]tmux.SessionState{
			"fresh-agent":    {Exists: true, HasLivePane: false},
			"attached-agent": {Exists: true, HasLivePane: false},
		},
		clients: map[string]bool{
			"fresh-agent":    false,
			"attached-agent": true,
		},
	}

	app := &App{
		tmuxAvailable: true,
		tmuxService:   newTmuxService(ops),
	}
	msg := app.gcStaleDetachedAgentSessions()()
	result, ok := msg.(staleDetachedAgentGCResult)
	if !ok {
		t.Fatalf("expected staleDetachedAgentGCResult, got %T", msg)
	}
	if result.Killed != 0 {
		t.Fatalf("expected killed=0, got %d", result.Killed)
	}
	if result.SkippedFresh != 1 {
		t.Fatalf("expected skipped_fresh=1, got %d", result.SkippedFresh)
	}
	if result.SkippedAttached != 1 {
		t.Fatalf("expected skipped_attached=1, got %d", result.SkippedAttached)
	}
}

func TestActivityTagTime_ParsesMixedUnits(t *testing.T) {
	base := time.Now().Add(-6 * time.Hour).Truncate(time.Second)
	secondsTS := base.Add(5 * time.Minute)
	millisTS := base.Add(20 * time.Minute)
	nanosTS := base.Add(45 * time.Minute)

	got := activityTagTime(map[string]string{
		tmux.TagLastOutputAt:   strconv.FormatInt(secondsTS.Unix(), 10),
		tmux.TagLastInputAt:    strconv.FormatInt(millisTS.UnixMilli(), 10),
		tmux.TagSessionLeaseAt: strconv.FormatInt(nanosTS.UnixNano(), 10),
	})
	if got.IsZero() {
		t.Fatal("expected non-zero activity time")
	}
	if got.UnixMilli() != nanosTS.UnixMilli() {
		t.Fatalf("expected newest mixed-unit tag time %d, got %d", nanosTS.UnixMilli(), got.UnixMilli())
	}
}

func TestActivityTagTime_CreatedAtFallback(t *testing.T) {
	created := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	got := activityTagTime(map[string]string{
		"@amux_created_at": strconv.FormatInt(created.Unix(), 10),
	})
	if got.IsZero() {
		t.Fatal("expected created_at fallback time")
	}
	if got.Unix() != created.Unix() {
		t.Fatalf("expected created_at fallback unix %d, got %d", created.Unix(), got.Unix())
	}
}
