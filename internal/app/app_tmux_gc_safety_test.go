package app

import (
	"fmt"
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
	sessionCreatedAt  func(name string, opts tmux.Options) (int64, error)
	killed            []string
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

func newGCTestApp(ops *gcOrphanOps) *App {
	return &App{
		tmuxAvailable:  true,
		projectsLoaded: true,
		tmuxService:    newTmuxService(ops),
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
					"@amux_created_at": fmt.Sprintf("%d", staleTS),
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
					"@amux_created_at": fmt.Sprintf("%d", recentTS),
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
					"@amux_created_at": fmt.Sprintf("%d", staleTS),
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

func TestGcOrphanedTmuxSessions_UsesInstanceScopedMatchWhenInstanceIDSet(t *testing.T) {
	var capturedMatches []map[string]string

	ops := &gcOrphanOps{
		sessionsWithTags: func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
			snapshot := make(map[string]string, len(match))
			for k, v := range match {
				snapshot[k] = v
			}
			capturedMatches = append(capturedMatches, snapshot)
			return nil, nil
		},
	}
	app := newGCTestApp(ops)
	app.instanceID = "test-instance-123"

	cmd := app.gcOrphanedTmuxSessions()
	_ = cmd()

	if len(capturedMatches) == 0 {
		t.Fatal("expected SessionsWithTags to be called")
	}
	foundScoped := false
	for _, capturedMatch := range capturedMatches {
		if capturedMatch["@amux"] == "1" && capturedMatch["@amux_instance"] == "test-instance-123" {
			foundScoped = true
			break
		}
	}
	if !foundScoped {
		t.Fatalf("expected at least one instance-scoped session query, got %v", capturedMatches)
	}
}

func TestGcOrphanedTmuxSessions_GlobalSweepPrunesCrossInstanceBacklog(t *testing.T) {
	now := time.Now()
	staleTS := strconv.FormatInt(now.Add(-2*time.Minute).Unix(), 10)

	ops := &gcOrphanOps{
		sessionsWithTags: func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
			// Primary pass: instance-scoped, no local orphans.
			if match["@amux_instance"] != "" {
				return nil, nil
			}
			// Session count query that decides whether to run a global sweep.
			if len(keys) == 0 {
				rows := make([]tmux.SessionTagValues, orphanGlobalSweepSessionThreshold)
				for i := range rows {
					rows[i] = tmux.SessionTagValues{Name: fmt.Sprintf("sess-%d", i)}
				}
				return rows, nil
			}
			// Global sweep should include cross-instance orphan sessions.
			return []tmux.SessionTagValues{
				{
					Name: "other-instance-stale",
					Tags: map[string]string{
						"@amux_workspace":  "dead-ws",
						"@amux_created_at": staleTS,
					},
				},
			}, nil
		},
		sessionHasClients: func(name string, opts tmux.Options) (bool, error) {
			return false, nil
		},
	}
	app := newGCTestApp(ops)
	app.instanceID = "my-instance"

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
		t.Fatalf("expected 1 killed in global sweep, got %d", result.Killed)
	}
	if len(ops.killed) != 1 || ops.killed[0] != "other-instance-stale" {
		t.Fatalf("expected other-instance-stale to be killed, got %v", ops.killed)
	}
}
