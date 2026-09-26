package process

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
)

// ErrSetupBusy reports that a setup sequence is already admitted for the
// workspace. It is a deliberate busy signal, not a failure: callers surface it
// informationally ("setup already running") rather than as an error.
var ErrSetupBusy = errors.New("setup already running for workspace")

// ErrWorkspaceTeardown reports that a teardown gate holds the workspace's
// lifecycle admission. While a service teardown (delete/shelve/project
// removal) holds the gate, new setup/run/archive/on-done admissions are
// rejected so nothing new starts mid-removal.
var ErrWorkspaceTeardown = errors.New("workspace teardown in progress")

// lifecycleTicket is one admitted setup sequence. The context spans the whole
// sequence — each command checks it before Start, and teardown cancels it once
// to abort the in-flight command plus every queued command alike. done closes
// when the sequence's owning goroutine fully exits; teardown drains on it.
type lifecycleTicket struct {
	gen    uint64
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	// cmd is the currently in-flight command, set/cleared under the
	// coordinator mutex so a teardown that lands mid-Start cannot miss it.
	cmd *exec.Cmd
}

// lifecycleProc is a tracked detached process (today: on-done hooks). done
// closes after the process is reaped so teardown can drain deterministically.
type lifecycleProc struct {
	cmd  *exec.Cmd
	done chan struct{}
}

// lifecycleState is the coordinator's per-workspace record, keyed by
// normalized workspace root (the same key as the running-script map).
type lifecycleState struct {
	gen      uint64                      // last admitted generation
	teardown bool                        // admission gate held by teardown
	setup    *lifecycleTicket            // in-flight setup sequence (<=1)
	onDone   map[*lifecycleProc]struct{} // detached on-done hooks
}

// lifecycleCoordinator owns local lifecycle-process admission and drain for
// every workspace: the single setup sequence (generation-guarded), detached
// on-done hooks, and the teardown gate that cancel/drains them before the
// workspace's directory is removed or shelved. Its mutex is independent of
// the runner's r.mu — process waits and kills run outside the lock.
type lifecycleCoordinator struct {
	mu      sync.Mutex
	nextGen uint64 // process-monotonic: stale tickets never match a fresh state
	states  map[string]*lifecycleState
	// killGroup routes process-group kills through the runner's injectable
	// seam so tests observe teardown kills without signaling real processes.
	killGroup func(pid int, opts KillOptions) error
}

func newLifecycleCoordinator() *lifecycleCoordinator {
	return &lifecycleCoordinator{
		states:    make(map[string]*lifecycleState),
		killGroup: KillProcessGroup,
	}
}

// stateFor returns (or creates) the workspace's lifecycle state.
func (c *lifecycleCoordinator) stateFor(key string) *lifecycleState {
	st := c.states[key]
	if st == nil {
		st = &lifecycleState{}
		c.states[key] = st
	}
	return st
}

// admitSetup reserves the workspace's single setup slot, minting a fresh
// generation. A second concurrent admission reports ErrSetupBusy; a held
// teardown gate reports ErrWorkspaceTeardown.
func (c *lifecycleCoordinator) admitSetup(key string) (*lifecycleTicket, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.stateFor(key)
	if st.teardown {
		return nil, ErrWorkspaceTeardown
	}
	if st.setup != nil {
		return nil, ErrSetupBusy
	}
	c.nextGen++
	ctx, cancel := context.WithCancel(context.Background())
	t := &lifecycleTicket{gen: c.nextGen, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	st.gen = t.gen
	st.setup = t
	return t, nil
}

// registerCmd records the just-started command as the ticket's in-flight
// process, atomically re-validating the ticket: returns false when teardown
// canceled the ticket between admission and Start — the caller must kill and
// reap the just-started process itself.
func (c *lifecycleCoordinator) registerCmd(key string, t *lifecycleTicket, cmd *exec.Cmd) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.states[key]
	if st == nil || st.setup != t || t.gen != st.gen || t.ctx.Err() != nil {
		return false
	}
	t.cmd = cmd
	return true
}

// clearCmd drops the finished command from the ticket.
func (c *lifecycleCoordinator) clearCmd(key string, t *lifecycleTicket, cmd *exec.Cmd) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if st := c.states[key]; st != nil && st.setup == t && t.cmd == cmd {
		t.cmd = nil
	}
}

// finishSetup releases the setup slot and closes the ticket's drain channel.
// A stale ticket (teardown bumped the generation) still closes its own done —
// teardown may be blocked waiting on it.
func (c *lifecycleCoordinator) finishSetup(key string, t *lifecycleTicket) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if st := c.states[key]; st != nil && st.setup == t {
		st.setup = nil
	}
	close(t.done)
	t.cancel()
}

// ticketCurrent reports whether the ticket still holds the workspace's setup
// generation — queued commands check it before starting.
func (c *lifecycleCoordinator) ticketCurrent(key string, t *lifecycleTicket) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.states[key]
	return st != nil && st.setup == t && st.gen == t.gen && t.ctx.Err() == nil
}

// admitOnDone registers a just-started on-done process for teardown tracking.
// Returns nil,false when the teardown gate is held — the caller must kill and
// reap the process itself.
func (c *lifecycleCoordinator) admitOnDone(key string, cmd *exec.Cmd) (*lifecycleProc, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.stateFor(key)
	if st.teardown {
		return nil, false
	}
	if st.onDone == nil {
		st.onDone = make(map[*lifecycleProc]struct{})
	}
	p := &lifecycleProc{cmd: cmd, done: make(chan struct{})}
	st.onDone[p] = struct{}{}
	return p, true
}

// finishOnDone unregisters a reaped on-done process and closes its drain
// channel so a waiting teardown can complete.
func (c *lifecycleCoordinator) finishOnDone(key string, p *lifecycleProc) {
	c.mu.Lock()
	if st := c.states[key]; st != nil {
		delete(st.onDone, p)
	}
	c.mu.Unlock()
	close(p.done)
}

// checkAdmission rejects new lifecycle starts while the teardown gate is held.
// Run scripts and any future startable hook consult it before spawning.
func (c *lifecycleCoordinator) checkAdmission(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if st := c.states[key]; st != nil && st.teardown {
		return ErrWorkspaceTeardown
	}
	return nil
}

// checkArchiveAdmission gates archive runs: while teardown is held only the
// owning guard may invoke archive — a foreign archive mid-teardown would write
// into a tree whose removal is already decided.
func (c *lifecycleCoordinator) checkArchiveAdmission(key string, g *TeardownGuard) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.states[key]
	if st == nil || !st.teardown {
		return nil
	}
	if g == nil || g.coord != c || g.key != key || g.released {
		return ErrWorkspaceTeardown
	}
	return nil
}

// live reports whether the workspace has any coordinator-tracked work in
// flight (setup sequence or on-done hooks) — feeds IsRunning so hosted-mode
// queries cannot hide active local lifecycle work.
func (c *lifecycleCoordinator) live(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.states[key]
	return st != nil && (st.setup != nil || len(st.onDone) > 0)
}

// TeardownGuard owns a workspace's lifecycle admission gate between teardown
// drain and the end of the service removal operation. While held, new
// lifecycle work is rejected; Finish releases the gate (failed operation) or
// drops the state entirely (workspace removed/shelved). released is guarded
// by the coordinator mutex — one lock domain, no lock ordering with the gate.
type TeardownGuard struct {
	coord    *lifecycleCoordinator
	runner   *ScriptRunner
	key      string
	released bool
}

// RunArchive runs the workspace's archive hook under the held teardown gate.
// It is the only archive admission allowed while the gate is held — and only
// while the guard is still valid.
func (g *TeardownGuard) RunArchive(ws *data.Workspace) error {
	if g == nil || g.runner == nil {
		return nil
	}
	return g.runner.runArchive(ws, g)
}

// Finish releases the teardown gate. removed=true drops the lifecycle state
// entirely (the workspace's tree is gone); removed=false re-opens admission
// for the surviving workspace while leaving the bumped generation in place so
// pre-teardown tickets stay permanently stale. Idempotent.
func (g *TeardownGuard) Finish(removed bool) {
	if g == nil || g.coord == nil {
		return
	}
	c := g.coord
	c.mu.Lock()
	defer c.mu.Unlock()
	if g.released {
		return
	}
	g.released = true
	st := c.states[g.key]
	if st == nil {
		return
	}
	if removed {
		delete(c.states, g.key)
		return
	}
	st.teardown = false
}

// snapshot captures the workspace's in-flight work under the lock: the setup
// ticket and the on-done set. Callers kill and drain the snapshot outside the
// lock.
type lifecycleSnapshot struct {
	ticket   *lifecycleTicket
	setupCmd *exec.Cmd
	procs    []*lifecycleProc
}

// beginTeardown marks the admission gate and bumps the generation so queued
// commands and stale tickets die, then snapshots the in-flight work. The
// caller kills/drains the snapshot; on failure it must call releaseTeardown
// so the surviving workspace stays usable.
func (c *lifecycleCoordinator) beginTeardown(key string) (*lifecycleSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.stateFor(key)
	if st.teardown {
		return nil, ErrWorkspaceTeardown
	}
	st.teardown = true
	c.nextGen++
	st.gen = c.nextGen
	snap := &lifecycleSnapshot{ticket: st.setup}
	if st.setup != nil {
		snap.setupCmd = st.setup.cmd
	}
	for p := range st.onDone {
		snap.procs = append(snap.procs, p)
	}
	return snap, nil
}

// releaseTeardown re-opens admission after a failed teardown: the gate drops
// but the bumped generation stays, so tickets admitted before the teardown
// remain stale.
func (c *lifecycleCoordinator) releaseTeardown(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if st := c.states[key]; st != nil {
		st.teardown = false
	}
}

// stopAllSnapshots snapshots every workspace's in-flight lifecycle work and
// clears the maps. Used by StopAll: no gate is set — the whole runner is going
// away, so new admissions racing shutdown are not a concern worth rejecting.
func (c *lifecycleCoordinator) stopAllSnapshots() []*lifecycleSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	var snaps []*lifecycleSnapshot
	for key, st := range c.states {
		snap := &lifecycleSnapshot{ticket: st.setup}
		if st.setup != nil {
			snap.setupCmd = st.setup.cmd
		}
		for p := range st.onDone {
			snap.procs = append(snap.procs, p)
		}
		snaps = append(snaps, snap)
		delete(c.states, key)
	}
	return snaps
}

// drainSnapshot cancels the ticket, kills every snapshot process group, and
// waits for each drain channel with SIGKILL escalation — mirroring Stop's
// kill-then-wait discipline so teardown only succeeds once nothing local is
// still writing. Any failure is returned to the caller, which must abort the
// destructive work.
func (c *lifecycleCoordinator) drainSnapshot(key string, snap *lifecycleSnapshot) error {
	if snap.ticket != nil {
		snap.ticket.cancel()
	}
	killGroup := c.killGroup
	if killGroup == nil {
		killGroup = KillProcessGroup
	}
	kill := func(cmd *exec.Cmd) error {
		if cmd == nil || cmd.Process == nil {
			return nil
		}
		return killGroup(cmd.Process.Pid, KillOptions{})
	}
	var firstErr error
	if err := kill(snap.setupCmd); err != nil && !isBenignStopError(err) {
		firstErr = err
	}
	for _, p := range snap.procs {
		if err := kill(p.cmd); err != nil && !isBenignStopError(err) && firstErr == nil {
			firstErr = err
		}
	}
	wait := func(done <-chan struct{}, cmd *exec.Cmd, what string) error {
		select {
		case <-done:
			return nil
		case <-time.After(scriptStopTimeout):
		}
		pid := 0
		if cmd != nil && cmd.Process != nil {
			pid = cmd.Process.Pid
		}
		logging.Warn("lifecycle drain timed out waiting for %s pid=%d key=%s; escalating to SIGKILL", what, pid, key)
		if cmd != nil && cmd.Process != nil {
			_ = ForceKillProcess(cmd.Process.Pid)
		}
		select {
		case <-done:
			return nil
		case <-time.After(scriptStopTimeout):
		}
		return fmt.Errorf("lifecycle drain timed out waiting for %s pid=%d", what, pid)
	}
	if snap.ticket != nil {
		if err := wait(snap.ticket.done, snap.setupCmd, "setup"); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, p := range snap.procs {
		if err := wait(p.done, p.cmd, "on-done"); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
