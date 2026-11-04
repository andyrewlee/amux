package center

import (
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestTabActorMachineFormallyChecked verifies the tab-actor liveness
// protocol table by exhaustive model checking.
func TestTabActorMachineFormallyChecked(t *testing.T) {
	if err := tabActorCheck(); err != nil {
		t.Fatalf("tab-actor liveness machine failed model checking: %v", err)
	}
}

// tabActorFixture builds a Model whose real fields derive to s, driving it
// there through the production methods wherever the protocol allows (the
// demoted-fresh anomaly is constructed by hand because only the stale-read
// race can produce it).
func tabActorFixture(t *testing.T, s tabActorState) (*Model, *int) {
	t.Helper()
	m := &Model{}
	delivered := new(int)
	m.msgSinkTry = func(tea.Msg) bool {
		*delivered++
		return true
	}
	switch s {
	case tabActorDetached, tabActorDetachedArmed:
		// Zero value: never attached.
	case tabActorLive, tabActorLiveArmed:
		m.setTabActorReady()
	case tabActorStalled, tabActorStalledArmed:
		m.setTabActorReady()
		atomic.StoreInt64(&m.tabActorHeartbeat, time.Now().Add(-2*tabActorStallTimeout).UnixNano())
		if m.isTabActorReady() {
			t.Fatalf("fixture %v: expected the stale read to demote", s)
		}
	case tabActorDemotedFresh, tabActorDemotedFreshArmed:
		// The race artifact: ready lost while the beat stayed fresh, warn
		// latch left set. Built directly — no single-threaded method path
		// reaches it.
		m.setTabActorReady()
		atomic.StoreUint32(&m.tabActorReady, 0)
		atomic.StoreUint32(&m.tabActorStalled, 1)
	}
	if s == tabActorDetachedArmed || s == tabActorLiveArmed ||
		s == tabActorStalledArmed || s == tabActorDemotedFreshArmed {
		m.requestTabActorRedraw()
	}
	if got := m.tabActorStateAt(time.Now()); got != s {
		t.Fatalf("fixture for %v derives %v", s, got)
	}
	return m, delivered
}

// TestTabActorRuntimeMatchesMachine differentially verifies the shipped
// atomics-and-methods implementation against the protocol table: for every
// declared state and every alphabet event, running the real method produces
// exactly the modeled transition, and where the table rejects the event the
// runtime leaves the derived state untouched.
func TestTabActorRuntimeMatchesMachine(t *testing.T) {
	run := func(m *Model, delivered *int, event tabActorEvent) {
		switch event {
		case tabActorEvtHeartbeat:
			m.noteTabActorHeartbeat()
		case tabActorEvtAttach:
			m.setTabActorReady()
		case tabActorEvtTimeoutDemote:
			// The demote read is time-driven: only a ready flag can be
			// demoted, so age the beat exactly in that case (detached has
			// no beat to age; demoted-fresh must keep its fresh beat).
			if atomic.LoadUint32(&m.tabActorReady) == 1 {
				atomic.StoreInt64(&m.tabActorHeartbeat, time.Now().Add(-2*tabActorStallTimeout).UnixNano())
			}
			m.isTabActorReady()
		case tabActorEvtStaleReadClobber:
			// The interleaving itself: the stale read's stores land over a
			// freshly-beaten actor (the heartbeat side already ran).
			atomic.StoreUint32(&m.tabActorReady, 0)
			atomic.CompareAndSwapUint32(&m.tabActorStalled, 0, 1)
		case tabActorEvtRedrawRequest:
			m.msgSinkTry = func(tea.Msg) bool {
				*delivered++
				return true
			}
			m.requestTabActorRedraw()
		case tabActorEvtRedrawRequestRejected:
			m.msgSinkTry = func(tea.Msg) bool {
				*delivered++
				return false
			}
			m.requestTabActorRedraw()
		case tabActorEvtRedrawConsume:
			m.clearTabActorRedrawPending()
		}
	}
	for _, state := range tabActorMachine.Reachable(nil) {
		for _, event := range tabActorMachine.Events() {
			modeled, ok := tabActorMachine.Next(state, event)
			m, delivered := tabActorFixture(t, state)
			before := *delivered
			run(m, delivered, event)
			got := m.tabActorStateAt(time.Now())
			if !ok {
				if got != state {
					t.Errorf("runtime invented transition %v --%v--> %v (table rejects)", state, event, got)
				}
				continue
			}
			if got != modeled {
				t.Errorf("runtime mismatch on %v --%v-->: got %v, model says %v", state, event, got, modeled)
			}
			// The coalescing guarantee: exactly one redraw message per
			// arming, none for a no-op re-request.
			switch event {
			case tabActorEvtRedrawRequest:
				want := 0
				if state == tabActorDetached || state == tabActorLive ||
					state == tabActorStalled || state == tabActorDemotedFresh {
					want = 1
				}
				if got := *delivered - before; got != want {
					t.Errorf("redraw request from %v delivered %d messages, want %d", state, got, want)
				}
			case tabActorEvtRedrawRequestRejected:
				if got := *delivered - before; got != 1 {
					t.Errorf("rejected request from %v reached the sink %d times, want 1", state, got)
				}
			}
		}
	}
}

// TestTabActorEpisodeLogsOnce pins the once-per-episode warn/recovery
// logging. The logging.Warn in isTabActorReady and the logging.Info in
// noteTabActorHeartbeat each sit on the same CAS arm as the observable
// tabActorStalled latch (model.go), so a latch flip is exactly a log line:
// asserting the latch transitions counts the logs without capturing the
// logger.
func TestTabActorEpisodeLogsOnce(t *testing.T) {
	m, _ := tabActorFixture(t, tabActorLive)

	// Demote: the warn latch arms exactly once, then stays armed.
	atomic.StoreInt64(&m.tabActorHeartbeat, time.Now().Add(-2*tabActorStallTimeout).UnixNano())
	if m.isTabActorReady() {
		t.Fatal("stale heartbeat must read as not ready")
	}
	if got := atomic.LoadUint32(&m.tabActorStalled); got != 1 {
		t.Fatalf("demote must arm the warn latch, got %d", got)
	}
	for i := 0; i < 5; i++ {
		if m.isTabActorReady() {
			t.Fatal("repeat demote reads must stay not ready")
		}
		if got := atomic.LoadUint32(&m.tabActorStalled); got != 1 {
			t.Fatalf("repeat demote read %d re-armed the latch: %d", i, got)
		}
	}

	// Recovery: the first heartbeat flips the latch (one recovery log);
	// further heartbeats change nothing.
	m.noteTabActorHeartbeat()
	if got := atomic.LoadUint32(&m.tabActorStalled); got != 0 {
		t.Fatalf("recovery heartbeat must clear the latch, got %d", got)
	}
	m.noteTabActorHeartbeat()
	m.noteTabActorHeartbeat()
	if got := atomic.LoadUint32(&m.tabActorStalled); got != 0 {
		t.Fatalf("live heartbeats must not touch the latch, got %d", got)
	}
	if !m.isTabActorReady() {
		t.Fatal("recovered actor must read as ready")
	}

	// A fresh episode may warn again.
	atomic.StoreInt64(&m.tabActorHeartbeat, time.Now().Add(-2*tabActorStallTimeout).UnixNano())
	if m.isTabActorReady() {
		t.Fatal("second stall must read as not ready")
	}
	if got := atomic.LoadUint32(&m.tabActorStalled); got != 1 {
		t.Fatalf("second episode must re-arm the latch, got %d", got)
	}

	// Attach ends the episode silently: the latch clears without any
	// heartbeat, so no recovery line is owed.
	m.setTabActorReady()
	if got := atomic.LoadUint32(&m.tabActorStalled); got != 0 {
		t.Fatalf("attach must reset the latch, got %d", got)
	}
}

// TestTabActorModelActivityFlushLatchBijection pins the documented cousin
// invariant: activityTagFlushPending is a pure bijection latch over the
// pending set — the pair mutates together under one mutex, so the flag
// equals "set non-empty" at every observation point. msgSink stays nil so
// markActivityTagForFlush never spawns a scheduler goroutine.
func TestTabActorModelActivityFlushLatchBijolution(t *testing.T) {
	m := &Model{}
	assert := func(wantFlag bool, wantLen int, stage string) {
		t.Helper()
		m.activityTagsMu.Lock()
		defer m.activityTagsMu.Unlock()
		gotLen := len(m.pendingActivityTags)
		if m.activityTagFlushPending != wantFlag || gotLen != wantLen {
			t.Fatalf("%s: flag=%v len=%d, want flag=%v len=%d",
				stage, m.activityTagFlushPending, gotLen, wantFlag, wantLen)
		}
	}
	assert(false, 0, "initial")
	m.markActivityTagForFlush("a")
	assert(true, 1, "first mark")
	m.markActivityTagForFlush("b")
	m.markActivityTagForFlush("a") // duplicate mark coalesces into the set
	assert(true, 2, "more marks")
	m.markActivityTagForFlush("") // empty name is rejected before the latch
	assert(true, 2, "empty mark")
	if got := m.drainActivityTags(); len(got) != 2 {
		t.Fatalf("drain returned %v, want both pending sessions", got)
	}
	assert(false, 0, "after drain")
	// Re-arm after a drain: the single slot takes the next mark.
	m.markActivityTagForFlush("c")
	assert(true, 1, "re-armed")
}
