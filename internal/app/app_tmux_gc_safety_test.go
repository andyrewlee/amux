package app

import (
	"strconv"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

type gcOrphanOps struct {
	rows          []tmux.SessionTagValues
	rowsErr       error
	createdAt     map[string]int64
	hasClients    map[string]bool
	hasClientsErr map[string]error
	killed        []string
	lastMatch     map[string]string
}

func (f *gcOrphanOps) EnsureAvailable() error { return nil }
func (f *gcOrphanOps) InstallHint() string    { return "" }
func (f *gcOrphanOps) ActiveAgentSessionsByActivity(time.Duration, tmux.Options) ([]tmux.SessionActivity, error) {
	return nil, nil
}

func (f *gcOrphanOps) SessionsWithTags(match map[string]string, _ []string, _ tmux.Options) ([]tmux.SessionTagValues, error) {
	if match != nil {
		f.lastMatch = make(map[string]string, len(match))
		for key, value := range match {
			f.lastMatch[key] = value
		}
	}
	if f.rowsErr != nil {
		return nil, f.rowsErr
	}
	return f.rows, nil
}

func (f *gcOrphanOps) SessionStateFor(string, tmux.Options) (tmux.SessionState, error) {
	return tmux.SessionState{}, nil
}

func (f *gcOrphanOps) SessionHasClients(sessionName string, _ tmux.Options) (bool, error) {
	if err := f.hasClientsErr[sessionName]; err != nil {
		return false, err
	}
	return f.hasClients[sessionName], nil
}

func (f *gcOrphanOps) SessionCreatedAt(sessionName string, _ tmux.Options) (int64, error) {
	return f.createdAt[sessionName], nil
}

func (f *gcOrphanOps) KillSession(sessionName string, _ tmux.Options) error {
	f.killed = append(f.killed, sessionName)
	return nil
}

func (f *gcOrphanOps) KillSessionsMatchingTags(map[string]string, tmux.Options) (bool, error) {
	return false, nil
}

func (f *gcOrphanOps) KillSessionsWithPrefix(string, tmux.Options) error { return nil }
func (f *gcOrphanOps) KillWorkspaceSessions(string, tmux.Options) error  { return nil }
func (f *gcOrphanOps) SetMonitorActivityOn(tmux.Options) error           { return nil }
func (f *gcOrphanOps) SetStatusOff(tmux.Options) error                   { return nil }
func (f *gcOrphanOps) CapturePaneTail(string, int, tmux.Options) (string, bool) {
	return "", false
}
func (f *gcOrphanOps) ContentHash(string) [16]byte { return [16]byte{} }

func TestGcOrphanedTmuxSessions_SkipsAttachedOrphans(t *testing.T) {
	createdAt := time.Now().Add(-time.Hour).Unix()
	ops := &gcOrphanOps{
		rows: []tmux.SessionTagValues{
			{
				Name: "attached-orphan",
				Tags: map[string]string{
					"@amux_workspace":  "dead-ws",
					"@amux_created_at": strconv.FormatInt(createdAt, 10),
				},
			},
		},
		hasClients: map[string]bool{
			"attached-orphan": true,
		},
	}
	app := &App{
		tmuxAvailable:  true,
		projectsLoaded: true,
		tmuxService:    newTmuxService(ops),
	}
	cmd := app.gcOrphanedTmuxSessions()
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
	msg := cmd()
	result, ok := msg.(orphanGCResult)
	if !ok {
		t.Fatalf("expected orphanGCResult, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("GC error: %v", result.Err)
	}
	if result.Killed != 0 {
		t.Fatalf("expected 0 killed, got %d", result.Killed)
	}
	if len(ops.killed) != 0 {
		t.Fatalf("expected no kill calls, got %v", ops.killed)
	}
}

func TestGcOrphanedTmuxSessions_SkipsRecentOrphans(t *testing.T) {
	ops := &gcOrphanOps{
		rows: []tmux.SessionTagValues{
			{
				Name: "recent-orphan",
				Tags: map[string]string{
					"@amux_workspace":  "dead-ws",
					"@amux_created_at": strconv.FormatInt(time.Now().Unix(), 10),
				},
			},
		},
		hasClients: map[string]bool{},
	}
	app := &App{
		tmuxAvailable:  true,
		projectsLoaded: true,
		tmuxService:    newTmuxService(ops),
	}
	cmd := app.gcOrphanedTmuxSessions()
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
	msg := cmd()
	result, ok := msg.(orphanGCResult)
	if !ok {
		t.Fatalf("expected orphanGCResult, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("GC error: %v", result.Err)
	}
	if result.Killed != 0 {
		t.Fatalf("expected 0 killed, got %d", result.Killed)
	}
	if len(ops.killed) != 0 {
		t.Fatalf("expected no kill calls, got %v", ops.killed)
	}
}

func TestGcOrphanedTmuxSessions_KillsStaleDetachedOrphans(t *testing.T) {
	createdAt := time.Now().Add(-(orphanSessionGracePeriod + time.Minute)).Unix()
	ops := &gcOrphanOps{
		rows: []tmux.SessionTagValues{
			{
				Name: "stale-orphan",
				Tags: map[string]string{
					"@amux_workspace":  "dead-ws",
					"@amux_created_at": strconv.FormatInt(createdAt, 10),
				},
			},
		},
		hasClients: map[string]bool{},
	}
	app := &App{
		tmuxAvailable:  true,
		projectsLoaded: true,
		tmuxService:    newTmuxService(ops),
	}
	cmd := app.gcOrphanedTmuxSessions()
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
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
		t.Fatalf("expected stale-orphan kill, got %v", ops.killed)
	}
}

func TestGcOrphanedTmuxSessions_SkipsRecentOrphansUsingTmuxCreatedAtFallback(t *testing.T) {
	ops := &gcOrphanOps{
		rows: []tmux.SessionTagValues{
			{
				Name: "untagged-recent",
				Tags: map[string]string{
					"@amux_workspace": "dead-ws",
				},
			},
		},
		createdAt: map[string]int64{
			"untagged-recent": time.Now().Unix(),
		},
		hasClients: map[string]bool{},
	}
	app := &App{
		tmuxAvailable:  true,
		projectsLoaded: true,
		tmuxService:    newTmuxService(ops),
	}
	cmd := app.gcOrphanedTmuxSessions()
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
	msg := cmd()
	result, ok := msg.(orphanGCResult)
	if !ok {
		t.Fatalf("expected orphanGCResult, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("GC error: %v", result.Err)
	}
	if result.Killed != 0 {
		t.Fatalf("expected 0 killed, got %d", result.Killed)
	}
	if len(ops.killed) != 0 {
		t.Fatalf("expected no kill calls, got %v", ops.killed)
	}
}

func TestGcOrphanedTmuxSessions_UsesInstanceScopedMatchWhenInstanceIDSet(t *testing.T) {
	ops := &gcOrphanOps{}
	app := &App{
		tmuxAvailable:  true,
		projectsLoaded: true,
		tmuxService:    newTmuxService(ops),
		instanceID:     "instance-a",
	}
	cmd := app.gcOrphanedTmuxSessions()
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
	msg := cmd()
	result, ok := msg.(orphanGCResult)
	if !ok {
		t.Fatalf("expected orphanGCResult, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("GC error: %v", result.Err)
	}
	if ops.lastMatch["@amux"] != "1" {
		t.Fatalf("expected @amux match tag, got %v", ops.lastMatch)
	}
	if ops.lastMatch["@amux_instance"] != "instance-a" {
		t.Fatalf("expected instance-scoped match, got %v", ops.lastMatch)
	}
}
