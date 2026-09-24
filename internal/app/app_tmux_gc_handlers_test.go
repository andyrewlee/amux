package app

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
)

// ---------------------------------------------------------------------------
// handleOrphanGCTick — drives the periodic GC sweep and re-arms the ticker.
//
// The function returns the Cmd slice that the Update loop will run. Its shape
// is fully determined by the gating flags on App: gcStaleDetachedAgentSessions
// is emitted whenever tmux is available, gcOrphanedTmuxSessions only once
// projects are also loaded, and the ticker is *always* appended last so the
// sweep keeps repeating. We assert on the slice these branches produce rather
// than running the ticker Cmd (it sleeps for orphanGCInterval).
// ---------------------------------------------------------------------------

// tickGCOps is a minimal tmuxService stub so the GC Cmds returned by
// handleOrphanGCTick resolve instantly to their result message when invoked,
// letting us assert their identity without touching a real tmux server.
type tickGCOps struct {
	tmuxOps
}

func (tickGCOps) SessionsWithTags(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
	return nil, nil
}

func (tickGCOps) AllSessionStates(tmux.Options) (map[string]tmux.SessionState, error) {
	return map[string]tmux.SessionState{}, nil
}

func (tickGCOps) AllSessionMeta(tmux.Options) (map[string]tmux.SessionMeta, error) {
	return map[string]tmux.SessionMeta{}, nil
}

func TestHandleOrphanGCTick_SkipsGCWhenTmuxUnavailable(t *testing.T) {
	app := &App{
		tmuxAvailable:  false,
		projectsLoaded: true,
		tmuxService:    tickGCOps{},
	}

	cmds := app.handleOrphanGCTick()

	// Both GC Cmds are gated off, so only the re-armed ticker remains.
	if len(cmds) != 1 {
		t.Fatalf("expected only the ticker Cmd when tmux unavailable, got %d cmds", len(cmds))
	}
	if cmds[0] == nil {
		t.Fatal("ticker Cmd must not be nil — the GC sweep would stop repeating")
	}
}

func TestHandleOrphanGCTick_RunsDetachedGCButNotOrphanGCBeforeProjectsLoad(t *testing.T) {
	app := &App{
		tmuxAvailable:  true,
		projectsLoaded: false,
		tmuxService:    tickGCOps{},
	}

	cmds := app.handleOrphanGCTick()

	// Detached-agent GC only needs tmux; orphan GC additionally needs projects
	// loaded. So before projects load we get detached GC + ticker = 2.
	if len(cmds) != 2 {
		t.Fatalf("expected detached GC + ticker (2 cmds) before projects load, got %d", len(cmds))
	}
	for i, cmd := range cmds {
		if cmd == nil {
			t.Fatalf("cmd[%d] is nil", i)
		}
	}

	// The single GC Cmd (everything before the trailing ticker) must be the
	// stale-detached-agent sweep, not the orphan sweep.
	msg := cmds[0]()
	if _, ok := msg.(staleDetachedAgentGCResult); !ok {
		t.Fatalf("expected first Cmd to be detached-agent GC, got %T", msg)
	}
}

func TestHandleOrphanGCTick_RunsBothGCAndRearmsTickerWhenReady(t *testing.T) {
	app := &App{
		tmuxAvailable:  true,
		projectsLoaded: true,
		tmuxService:    tickGCOps{},
	}

	cmds := app.handleOrphanGCTick()

	// Ready state: detached GC + orphan GC + ticker.
	if len(cmds) != 3 {
		t.Fatalf("expected detached GC + orphan GC + ticker (3 cmds), got %d", len(cmds))
	}
	for i, cmd := range cmds {
		if cmd == nil {
			t.Fatalf("cmd[%d] is nil", i)
		}
	}

	// The ticker is always appended last, so the leading Cmds are the two GC
	// sweeps. Invoking them (safe with the stub) must yield exactly one of each
	// GC result type — proving both sweeps were wired in, in either order.
	gotDetached, gotOrphan := false, false
	for _, cmd := range cmds[:len(cmds)-1] {
		switch cmd().(type) {
		case staleDetachedAgentGCResult:
			gotDetached = true
		case orphanGCResult:
			gotOrphan = true
		default:
			t.Fatalf("unexpected non-GC Cmd among the leading sweep Cmds")
		}
	}
	if !gotDetached {
		t.Error("stale-detached-agent GC sweep was not scheduled")
	}
	if !gotOrphan {
		t.Error("orphan tmux GC sweep was not scheduled")
	}
}

func TestHandleTmuxAvailableResult_RunsStartupCleanupWhenProjectsReady(t *testing.T) {
	app := &App{
		projectsLoaded: true,
		tmuxService:    tickGCOps{},
	}

	cmds := app.handleTmuxAvailableResult(tmuxAvailableResult{available: true})
	if len(cmds) < 2 {
		t.Fatalf("expected startup cleanup commands, got %d", len(cmds))
	}
	msg := cmds[len(cmds)-2]()
	if _, ok := msg.(staleDetachedAgentGCResult); !ok {
		t.Fatalf("penultimate startup command returned %T, want staleDetachedAgentGCResult", msg)
	}
	msg = cmds[len(cmds)-1]()
	if _, ok := msg.(orphanGCResult); !ok {
		t.Fatalf("last startup command returned %T, want orphanGCResult", msg)
	}
}

// ---------------------------------------------------------------------------
// handleStaleDetachedAgentGCResult — logs the sweep outcome. We run it against
// the real logging backend (pointed at a temp file) and read the emitted lines
// back, asserting the correct branch fired: a WARN on error, an INFO counter
// breakdown when sessions were killed, and silence otherwise. Driving the real
// logger also validates the format-string argument counts.
// ---------------------------------------------------------------------------

func TestHandleStaleDetachedAgentGCResult(t *testing.T) {
	tests := []struct {
		name        string
		msg         staleDetachedAgentGCResult
		wantLevel   string   // "WARN", "INFO", or "" for no log line
		wantSubstrs []string // fragments that must appear on the logged line
	}{
		{
			name:        "error path logs warning",
			msg:         staleDetachedAgentGCResult{Err: errors.New("boom")},
			wantLevel:   "WARN",
			wantSubstrs: []string{"detached agent GC", "boom"},
		},
		{
			name: "kills logged with full counter breakdown",
			msg: staleDetachedAgentGCResult{
				Considered:      5,
				Killed:          2,
				SkippedAttached: 1,
				SkippedFresh:    1,
				SkippedLivePane: 1,
			},
			wantLevel: "INFO",
			wantSubstrs: []string{
				"killed=2", "considered=5", "attached=1", "fresh=1", "live_pane=1",
			},
		},
		{
			name:      "zero kills is a quiet no-op",
			msg:       staleDetachedAgentGCResult{Considered: 3, Killed: 0, SkippedFresh: 3},
			wantLevel: "",
		},
		{
			name:      "empty result is a quiet no-op",
			msg:       staleDetachedAgentGCResult{},
			wantLevel: "",
		},
		{
			name:        "error wins even when counters are populated",
			msg:         staleDetachedAgentGCResult{Killed: 9, Err: activity.ErrTmuxUnavailable},
			wantLevel:   "WARN",
			wantSubstrs: []string{"detached agent GC"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logPath := withTempLogger(t)
			app := &App{tmuxAvailable: true, instanceID: "inst"}

			app.handleStaleDetachedAgentGCResult(tt.msg)

			assertLogLine(t, logPath, tt.wantLevel, tt.wantSubstrs)
		})
	}
}

// ---------------------------------------------------------------------------
// handleSessionCountResult — logs the startup session count. Same contract:
// a WARN on error, otherwise an INFO line carrying the count (including zero).
// ---------------------------------------------------------------------------

func TestHandleSessionCountResult(t *testing.T) {
	tests := []struct {
		name        string
		msg         sessionCountResult
		wantLevel   string
		wantSubstrs []string
	}{
		{
			name:        "error path logs warning",
			msg:         sessionCountResult{Err: errors.New("count failed")},
			wantLevel:   "WARN",
			wantSubstrs: []string{"session count", "count failed"},
		},
		{
			name:        "zero count is still logged",
			msg:         sessionCountResult{Count: 0},
			wantLevel:   "INFO",
			wantSubstrs: []string{"session count at startup: 0"},
		},
		{
			name:        "positive count is logged",
			msg:         sessionCountResult{Count: 7},
			wantLevel:   "INFO",
			wantSubstrs: []string{"session count at startup: 7"},
		},
		{
			name:        "error wins even when a count is present",
			msg:         sessionCountResult{Count: 4, Err: activity.ErrTmuxUnavailable},
			wantLevel:   "WARN",
			wantSubstrs: []string{"session count"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logPath := withTempLogger(t)
			app := &App{tmuxAvailable: true}

			app.handleSessionCountResult(tt.msg)

			assertLogLine(t, logPath, tt.wantLevel, tt.wantSubstrs)
		})
	}
}

// withTempLogger initializes the real logging backend against a throwaway
// directory so the GC handlers exercise their actual logging.Info/Warn calls
// (catching format-string mismatches) without writing into the user's logs.
// It returns the path of the active log file so tests can read it back.
func withTempLogger(t *testing.T) string {
	t.Helper()
	if err := logging.Initialize(t.TempDir(), logging.LevelDebug); err != nil {
		t.Fatalf("init logger: %v", err)
	}
	t.Cleanup(func() { logging.SetEnabled(false) })
	path := logging.GetLogPath()
	if path == "" {
		t.Fatal("expected a non-empty log path after Initialize")
	}
	return path
}

// assertLogLine reads the temp log file and checks for the expected outcome.
// wantLevel "" asserts no log line was emitted at all; otherwise it asserts a
// single line tagged with wantLevel containing every fragment in wantSubstrs.
func assertLogLine(t *testing.T, logPath, wantLevel string, wantSubstrs []string) {
	t.Helper()

	if err := logging.Close(); err != nil {
		t.Fatalf("close logger: %v", err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	content := strings.TrimSpace(string(raw))

	if wantLevel == "" {
		if content != "" {
			t.Fatalf("expected no log output, got:\n%s", content)
		}
		return
	}

	lines := strings.Split(content, "\n")
	if len(lines) != 1 {
		t.Fatalf("expected exactly one log line, got %d:\n%s", len(lines), content)
	}
	line := lines[0]
	if !strings.Contains(line, wantLevel+":") {
		t.Fatalf("expected %s-level log line, got: %q", wantLevel, line)
	}
	for _, sub := range wantSubstrs {
		if !strings.Contains(line, sub) {
			t.Fatalf("expected log line to contain %q, got: %q", sub, line)
		}
	}
}

// ---------------------------------------------------------------------------
// Orphan GC cadence (plan 085): the sweep must run on its dedicated 60s ticker,
// not piggyback on the 7s sync tick where it re-issued the session listings
// the tick already ran (~8.6× the designed fork cost).
// ---------------------------------------------------------------------------

// countingGCListingOps counts the batched session-listing calls each GC sweep
// issues, so tests can assert exactly which sweeps a tick's Cmds perform.
type countingGCListingOps struct {
	tmuxOps
	mu               sync.Mutex
	sessionsWithTags int
}

func (o *countingGCListingOps) SessionsWithTags(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sessionsWithTags++
	return nil, nil
}

func (o *countingGCListingOps) AllSessionStates(tmux.Options) (map[string]tmux.SessionState, error) {
	return map[string]tmux.SessionState{}, nil
}

func (o *countingGCListingOps) AllSessionMeta(tmux.Options) (map[string]tmux.SessionMeta, error) {
	return map[string]tmux.SessionMeta{}, nil
}

func (o *countingGCListingOps) tagListingCalls() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.sessionsWithTags
}

// runTickCmds invokes every returned Cmd except the last — by convention the
// re-armed ticker is appended last and would sleep for the full interval.
func runTickCmds(cmds []tea.Cmd) {
	for _, cmd := range cmds[:len(cmds)-1] {
		if cmd != nil {
			cmd()
		}
	}
}

func TestTmuxSyncTick_DoesNotRunOrphanGC(t *testing.T) {
	ops := &countingGCListingOps{}
	app := &App{
		tmuxAvailable:   true,
		projectsLoaded:  true,
		tmuxService:     ops,
		activeWorkspace: &data.Workspace{Name: "ws", Root: t.TempDir()},
	}
	app.tmuxActivity.syncToken = 7

	cmds := app.handleTmuxSyncTick(messages.TmuxSyncTick{Token: 7})
	if len(cmds) < 2 {
		t.Fatalf("expected discovery+sync Cmds plus the ticker, got %d", len(cmds))
	}
	runTickCmds(cmds)

	// Discovery issues exactly one SessionsWithTags; a piggybacked orphan GC
	// would add a second via amuxSessionsByWorkspace.
	if got := ops.tagListingCalls(); got != 1 {
		t.Fatalf("sync tick issued %d session-tag listings, want 1 (discovery only — orphan GC must stay on its 60s ticker)", got)
	}
}

func TestOrphanGCTick_StillRunsOrphanGC(t *testing.T) {
	ops := &countingGCListingOps{}
	app := &App{
		tmuxAvailable:  true,
		projectsLoaded: true,
		tmuxService:    ops,
	}

	cmds := app.handleOrphanGCTick()
	if len(cmds) != 3 {
		t.Fatalf("expected stale-detached + orphan GC Cmds plus ticker, got %d", len(cmds))
	}
	runTickCmds(cmds)

	// gcStaleDetachedAgentSessions and gcOrphanedTmuxSessions each issue one
	// SessionsWithTags listing.
	if got := ops.tagListingCalls(); got != 2 {
		t.Fatalf("orphan GC tick issued %d session-tag listings, want 2 (stale-detached + orphan sweeps)", got)
	}
}
