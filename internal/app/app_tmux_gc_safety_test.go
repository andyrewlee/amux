package app

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// gcOrphanOps is a mock TmuxOps for GC safety tests.
type gcOrphanOps struct {
	tmuxOps // embed default no-ops

	sessionsWithTags  func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error)
	sessionHasClients func(name string, opts tmux.Options) (bool, error)
	sessionStateFor   func(name string, opts tmux.Options) (tmux.SessionState, error)
	sessionCreatedAt  func(name string, opts tmux.Options) (int64, error)
	killed            []string
	heartbeatSessions []string
	heartbeatKey      string
	heartbeatValue    string
}

func (g *gcOrphanOps) SessionsWithTags(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
	if g.sessionsWithTags != nil {
		return g.sessionsWithTags(match, keys, opts)
	}
	return nil, nil
}

func (g *gcOrphanOps) SessionHasClients(name string, opts tmux.Options) (bool, error) {
	if g.sessionHasClients != nil {
		return g.sessionHasClients(name, opts)
	}
	return false, nil
}

func (g *gcOrphanOps) SessionStateFor(name string, opts tmux.Options) (tmux.SessionState, error) {
	if g.sessionStateFor != nil {
		return g.sessionStateFor(name, opts)
	}
	return tmux.SessionState{}, nil
}

func (g *gcOrphanOps) SessionCreatedAt(name string, opts tmux.Options) (int64, error) {
	if g.sessionCreatedAt != nil {
		return g.sessionCreatedAt(name, opts)
	}
	return 0, nil
}

func (g *gcOrphanOps) KillSession(name string, opts tmux.Options) error {
	g.killed = append(g.killed, name)
	return nil
}

func (g *gcOrphanOps) SetSessionTagValueForSessions(sessionNames []string, key, value string, _ tmux.Options) error {
	g.heartbeatSessions = append(g.heartbeatSessions, sessionNames...)
	g.heartbeatKey = key
	g.heartbeatValue = value
	return nil
}

func newGCTestApp(ops *gcOrphanOps) *App {
	return &App{
		tmuxAvailable:  true,
		projectsLoaded: true,
		instanceID:     "aaaaaaaaaaaaaaaa.1111111111111111",
		tmuxService:    ops,
	}
}

func TestGcOrphanedTmuxSessions_SkipsAttachedOrphans(t *testing.T) {
	now := time.Now()
	staleTS := now.Add(-2 * time.Minute).Unix()

	ops := &gcOrphanOps{
		sessionsWithTags: func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
			return []tmux.SessionTagValues{
				{Name: "attached-orphan", Tags: map[string]string{
					"@amux_workspace":  "dead-ws",
					"@amux_created_at": strconv.FormatInt(staleTS, 10),
				}},
			}, nil
		},
		sessionHasClients: func(name string, opts tmux.Options) (bool, error) {
			return true, nil // has attached clients
		},
	}
	app := newGCTestApp(ops)

	cmd := app.gcOrphanedTmuxSessions()
	msg := cmd()
	result, ok := msg.(orphanGCResult)
	if !ok {
		t.Fatalf("expected orphanGCResult, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("GC error: %v", result.Err)
	}
	if result.Killed != 0 {
		t.Fatalf("expected 0 killed (attached), got %d", result.Killed)
	}
	if len(ops.killed) != 0 {
		t.Fatalf("expected no kills, got %v", ops.killed)
	}
}

func TestGcOrphanedTmuxSessions_SkipsRecentOrphans(t *testing.T) {
	now := time.Now()
	recentTS := now.Add(-5 * time.Second).Unix()

	ops := &gcOrphanOps{
		sessionsWithTags: func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
			return []tmux.SessionTagValues{
				{Name: "recent-orphan", Tags: map[string]string{
					"@amux_workspace":  "dead-ws",
					"@amux_created_at": strconv.FormatInt(recentTS, 10),
				}},
			}, nil
		},
	}
	app := newGCTestApp(ops)

	cmd := app.gcOrphanedTmuxSessions()
	msg := cmd()
	result, ok := msg.(orphanGCResult)
	if !ok {
		t.Fatalf("expected orphanGCResult, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("GC error: %v", result.Err)
	}
	if result.Killed != 0 {
		t.Fatalf("expected 0 killed (recent), got %d", result.Killed)
	}
	if len(ops.killed) != 0 {
		t.Fatalf("expected no kills, got %v", ops.killed)
	}
}

func TestGcOrphanedTmuxSessions_KillsStaleDetachedOrphans(t *testing.T) {
	now := time.Now()
	staleTS := now.Add(-2 * time.Minute).Unix()

	ops := &gcOrphanOps{
		sessionsWithTags: func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
			return []tmux.SessionTagValues{
				{Name: "stale-orphan", Tags: map[string]string{
					"@amux_workspace":  "dead-ws",
					"@amux_created_at": strconv.FormatInt(staleTS, 10),
				}},
			}, nil
		},
		sessionHasClients: func(name string, opts tmux.Options) (bool, error) {
			return false, nil // no clients
		},
	}
	app := newGCTestApp(ops)

	cmd := app.gcOrphanedTmuxSessions()
	msg := cmd()
	result, ok := msg.(orphanGCResult)
	if !ok {
		t.Fatalf("expected orphanGCResult, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("GC error: %v", result.Err)
	}
	if result.Killed != 1 {
		t.Fatalf("expected 1 killed, got %d", result.Killed)
	}
	if len(ops.killed) != 1 || ops.killed[0] != "stale-orphan" {
		t.Fatalf("expected stale-orphan killed, got %v", ops.killed)
	}
}

func TestGcOrphanedTmuxSessions_SkipsDetachedOrphanWithLivePane(t *testing.T) {
	now := time.Now()
	ops := &gcOrphanOps{
		sessionsWithTags: func(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
			return []tmux.SessionTagValues{{
				Name: "running-orphan",
				Tags: map[string]string{
					"@amux_workspace":  "missing-workspace",
					"@amux_created_at": strconv.FormatInt(now.Add(-2*time.Minute).Unix(), 10),
				},
			}}, nil
		},
		sessionHasClients: func(string, tmux.Options) (bool, error) { return false, nil },
		sessionStateFor: func(string, tmux.Options) (tmux.SessionState, error) {
			return tmux.SessionState{Exists: true, HasLivePane: true}, nil
		},
	}
	app := newGCTestApp(ops)

	result, ok := app.gcOrphanedTmuxSessions()().(orphanGCResult)
	if !ok {
		t.Fatal("GC did not return orphanGCResult")
	}
	if result.Killed != 0 || len(ops.killed) != 0 {
		t.Fatalf("live orphan was killed: result=%+v killed=%v", result, ops.killed)
	}
}

func TestGcOrphanedTmuxSessions_FailsClosedOnPaneLivenessError(t *testing.T) {
	now := time.Now()
	ops := &gcOrphanOps{
		sessionsWithTags: func(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
			return []tmux.SessionTagValues{{
				Name: "unverifiable-orphan",
				Tags: map[string]string{
					"@amux_workspace":  "missing-workspace",
					"@amux_created_at": strconv.FormatInt(now.Add(-2*time.Minute).Unix(), 10),
				},
			}}, nil
		},
		sessionHasClients: func(string, tmux.Options) (bool, error) { return false, nil },
		sessionStateFor: func(string, tmux.Options) (tmux.SessionState, error) {
			return tmux.SessionState{}, errors.New("pane query failed")
		},
	}
	app := newGCTestApp(ops)

	result, ok := app.gcOrphanedTmuxSessions()().(orphanGCResult)
	if !ok {
		t.Fatal("GC did not return orphanGCResult")
	}
	if result.Killed != 0 || len(ops.killed) != 0 {
		t.Fatalf("unverifiable orphan was killed: result=%+v killed=%v", result, ops.killed)
	}
}

func TestGcOrphanedTmuxSessions_SkipsRecentOrphansUsingTmuxCreatedAtFallback(t *testing.T) {
	now := time.Now()
	recentTS := now.Add(-5 * time.Second).Unix()

	ops := &gcOrphanOps{
		sessionsWithTags: func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
			return []tmux.SessionTagValues{
				{Name: "no-tag-orphan", Tags: map[string]string{
					"@amux_workspace": "dead-ws",
					// no @amux_created_at tag
				}},
			}, nil
		},
		sessionCreatedAt: func(name string, opts tmux.Options) (int64, error) {
			return recentTS, nil // tmux fallback returns recent timestamp
		},
	}
	app := newGCTestApp(ops)

	cmd := app.gcOrphanedTmuxSessions()
	msg := cmd()
	result, ok := msg.(orphanGCResult)
	if !ok {
		t.Fatalf("expected orphanGCResult, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("GC error: %v", result.Err)
	}
	if result.Killed != 0 {
		t.Fatalf("expected 0 killed (recent via fallback), got %d", result.Killed)
	}
}

func TestGcOrphanedTmuxSessions_EnumeratesInstancesForOwnershipReconciliation(t *testing.T) {
	var capturedMatch map[string]string

	ops := &gcOrphanOps{
		sessionsWithTags: func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
			capturedMatch = match
			return nil, nil
		},
	}
	app := newGCTestApp(ops)
	app.instanceID = "aaaaaaaaaaaaaaaa.1111111111111111"

	cmd := app.gcOrphanedTmuxSessions()
	_ = cmd()

	if capturedMatch == nil {
		t.Fatal("expected SessionsWithTags to be called")
	}
	if capturedMatch["@amux"] != "1" {
		t.Fatalf("expected @amux=1, got %v", capturedMatch["@amux"])
	}
	if _, filtered := capturedMatch["@amux_instance"]; filtered {
		t.Fatalf("expected no @amux_instance filter, got %v", capturedMatch)
	}
}

func TestGcOrphanedTmuxSessions_ProtectsOnlyLiveForeignOwner(t *testing.T) {
	now := time.Now()
	for _, tt := range []struct {
		name      string
		heartbeat time.Time
		wantKill  int
	}{
		{name: "fresh owner heartbeat", heartbeat: now, wantKill: 0},
		{name: "stale owner heartbeat", heartbeat: now.Add(-sessionOwnerStaleAfter - time.Minute), wantKill: 1},
		{name: "missing owner heartbeat", wantKill: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tags := map[string]string{
				"@amux_workspace":  "foreign-workspace",
				"@amux_instance":   "aaaaaaaaaaaaaaaa.2222222222222222",
				"@amux_created_at": strconv.FormatInt(now.Add(-2*time.Minute).Unix(), 10),
			}
			if !tt.heartbeat.IsZero() {
				tags[tmux.TagSessionOwnerHeartbeatAt] = strconv.FormatInt(tt.heartbeat.UnixMilli(), 10)
			}
			ops := &gcOrphanOps{
				sessionsWithTags: func(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
					return []tmux.SessionTagValues{{Name: "foreign-session", Tags: tags}}, nil
				},
				sessionHasClients: func(string, tmux.Options) (bool, error) { return false, nil },
			}
			app := newGCTestApp(ops)
			app.instanceID = "aaaaaaaaaaaaaaaa.1111111111111111"

			msg := app.gcOrphanedTmuxSessions()()
			result, ok := msg.(orphanGCResult)
			if !ok {
				t.Fatalf("GC returned %T, want orphanGCResult", msg)
			}
			if result.Killed != tt.wantKill {
				t.Fatalf("killed = %d, want %d", result.Killed, tt.wantKill)
			}
		})
	}
}

func TestGcOrphanedTmuxSessions_RefreshesCurrentOwnerHeartbeat(t *testing.T) {
	now := time.Now()
	ops := &gcOrphanOps{
		sessionsWithTags: func(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
			return []tmux.SessionTagValues{{
				Name: "owned-session",
				Tags: map[string]string{
					"@amux_workspace":  "new-workspace",
					"@amux_instance":   "aaaaaaaaaaaaaaaa.1111111111111111",
					"@amux_created_at": strconv.FormatInt(now.Unix(), 10),
				},
			}}, nil
		},
	}
	app := newGCTestApp(ops)
	app.instanceID = "aaaaaaaaaaaaaaaa.1111111111111111"

	msg := app.gcOrphanedTmuxSessions()()
	result, ok := msg.(orphanGCResult)
	if !ok {
		t.Fatalf("GC returned %T, want orphanGCResult", msg)
	}
	if result.Err != nil {
		t.Fatalf("GC error: %v", result.Err)
	}
	if len(ops.heartbeatSessions) != 1 || ops.heartbeatSessions[0] != "owned-session" {
		t.Fatalf("heartbeat sessions = %v", ops.heartbeatSessions)
	}
	if ops.heartbeatKey != tmux.TagSessionOwnerHeartbeatAt || ops.heartbeatValue == "" {
		t.Fatalf("heartbeat write = key %q value %q", ops.heartbeatKey, ops.heartbeatValue)
	}
}

func TestGcOrphanedTmuxSessions_IgnoresDifferentStateNamespace(t *testing.T) {
	calledClients := false
	ops := &gcOrphanOps{
		sessionsWithTags: func(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
			return []tmux.SessionTagValues{{
				Name: "other-profile-session",
				Tags: map[string]string{
					"@amux_workspace": "other-workspace",
					"@amux_instance":  "bbbbbbbbbbbbbbbb.2222222222222222",
				},
			}}, nil
		},
		sessionHasClients: func(string, tmux.Options) (bool, error) {
			calledClients = true
			return false, nil
		},
	}
	app := newGCTestApp(ops)
	app.instanceID = "aaaaaaaaaaaaaaaa.1111111111111111"

	msg := app.gcOrphanedTmuxSessions()()
	result, ok := msg.(orphanGCResult)
	if !ok {
		t.Fatalf("GC returned %T, want orphanGCResult", msg)
	}
	if result.Killed != 0 || len(ops.killed) != 0 || calledClients {
		t.Fatalf("different namespace was considered for cleanup: result=%+v killed=%v clients=%v", result, ops.killed, calledClients)
	}
}

func TestGcOrphanedTmuxSessions_SkipsDeleteInFlightOrphan(t *testing.T) {
	now := time.Now()
	staleTS := now.Add(-2 * time.Minute).Unix()

	ops := &gcOrphanOps{
		sessionsWithTags: func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
			return []tmux.SessionTagValues{
				{Name: "deleting-orphan", Tags: map[string]string{
					"@amux_workspace":  "dead-ws",
					"@amux_created_at": strconv.FormatInt(staleTS, 10),
				}},
			}, nil
		},
		sessionHasClients: func(name string, opts tmux.Options) (bool, error) {
			return false, nil // detached
		},
	}
	app := newGCTestApp(ops)
	// dead-ws is mid-delete and already absent from a.projects; orphan GC must
	// treat it as known and leave its session for the orderly delete cleanup.
	app.lifecycle.phases = map[string]lifecyclePhase{"dead-ws": lifecycleDeleting}

	msg := app.gcOrphanedTmuxSessions()()
	result, ok := msg.(orphanGCResult)
	if !ok {
		t.Fatalf("expected orphanGCResult, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("GC error: %v", result.Err)
	}
	if result.Killed != 0 {
		t.Fatalf("expected 0 killed for a delete-in-flight orphan, got %d", result.Killed)
	}
	if len(ops.killed) != 0 {
		t.Fatalf("expected no sessions killed, got %v", ops.killed)
	}
}

func TestGcOrphanedTmuxSessions_FailsClosedOnClientCheckError(t *testing.T) {
	now := time.Now()
	staleTS := now.Add(-2 * time.Minute).Unix()

	ops := &gcOrphanOps{
		sessionsWithTags: func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
			return []tmux.SessionTagValues{
				{Name: "unverifiable-orphan", Tags: map[string]string{
					"@amux_workspace":  "dead-ws",
					"@amux_created_at": strconv.FormatInt(staleTS, 10),
				}},
			}, nil
		},
		sessionHasClients: func(name string, opts tmux.Options) (bool, error) {
			return false, errors.New("list-clients raced session teardown")
		},
	}
	app := newGCTestApp(ops)

	msg := app.gcOrphanedTmuxSessions()()
	result, ok := msg.(orphanGCResult)
	if !ok {
		t.Fatalf("expected orphanGCResult, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("GC error: %v", result.Err)
	}
	if result.Killed != 0 {
		t.Fatalf("expected 0 killed on unverifiable client check, got %d", result.Killed)
	}
	if len(ops.killed) != 0 {
		t.Fatalf("expected no kills on unverifiable client check, got %v", ops.killed)
	}
}
