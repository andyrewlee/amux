package git

import (
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/fsm"
)

// The debounce/trailing-timer model.
//
// handleChange + armTrailingTimerLocked + fireTrailing implement an
// implicit two-state machine per watched root: idle (no trailing timer
// pending) and armed (a trailing timer pending; its generation number is
// extended state). The policy: an fs event at least one debounce past the
// last fire fires immediately and cancels any pending timer; a burst event
// (re)arms the trailing timer, superseding older generations; only the
// current generation's fire may run the callback — a superseded or
// canceled timer that races in is ignored; Unwatch/Close cancels. The
// machine below is that policy's checked spec; the differential test then
// drives the REAL watcher through the same event sequences and asserts the
// two agree on every fire and on armed-ness at every step.

type watchDebounceState uint8

const (
	watchDebounceIdle  watchDebounceState = iota // no trailing timer pending
	watchDebounceArmed                           // trailing timer pending (generation N)
)

func (s watchDebounceState) String() string {
	if s == watchDebounceArmed {
		return "armed"
	}
	return "idle"
}

type watchDebounceEvent uint8

const (
	// evtFsEventQuiet: an fs event at least one debounce after the last
	// fire — the callback runs now, any pending timer is canceled.
	evtFsEventQuiet watchDebounceEvent = iota
	// evtFsEventRapid: an fs event inside the debounce window — the
	// trailing timer (re)arms, superseding previous generations.
	evtFsEventRapid
	// evtTrailingFireCurrent: the pending generation's timer fired — the
	// callback runs and the machine disarms.
	evtTrailingFireCurrent
	// evtTrailingFireStale: a superseded or canceled generation's timer
	// raced in — ignored.
	evtTrailingFireStale
	// evtDebounceCancel: Unwatch/Close — the timer is stopped; no callback
	// can ever run for it (a late fire observes a generation mismatch).
	evtDebounceCancel
)

func (e watchDebounceEvent) String() string {
	switch e {
	case evtFsEventQuiet:
		return "fs-event-quiet"
	case evtFsEventRapid:
		return "fs-event-rapid"
	case evtTrailingFireCurrent:
		return "trailing-fire-current"
	case evtTrailingFireStale:
		return "trailing-fire-stale"
	default:
		return "cancel"
	}
}

var watchDebounceMachine = fsm.MustCompile(fsm.Machine[watchDebounceState, watchDebounceEvent]{
	Name:    "watcher-debounce",
	Initial: watchDebounceIdle,
	Steps: []fsm.Step[watchDebounceState, watchDebounceEvent]{
		{From: watchDebounceIdle, Event: evtFsEventQuiet, To: watchDebounceIdle},
		{From: watchDebounceIdle, Event: evtFsEventRapid, To: watchDebounceArmed},
		{From: watchDebounceIdle, Event: evtTrailingFireStale, To: watchDebounceIdle},
		{From: watchDebounceIdle, Event: evtDebounceCancel, To: watchDebounceIdle},
		{From: watchDebounceArmed, Event: evtFsEventQuiet, To: watchDebounceIdle},
		{From: watchDebounceArmed, Event: evtFsEventRapid, To: watchDebounceArmed},
		{From: watchDebounceArmed, Event: evtTrailingFireCurrent, To: watchDebounceIdle},
		{From: watchDebounceArmed, Event: evtTrailingFireStale, To: watchDebounceArmed},
		{From: watchDebounceArmed, Event: evtDebounceCancel, To: watchDebounceIdle},
	},
})

// watchDebounceFires reports whether an edge runs the onChanged callback:
// exactly the immediate (quiet) edges and the current-generation trailing
// fire. Stale fires and cancels never do.
func watchDebounceFires(from watchDebounceState, event watchDebounceEvent) bool {
	switch {
	case event == evtFsEventQuiet:
		return true
	case from == watchDebounceArmed && event == evtTrailingFireCurrent:
		return true
	default:
		return false
	}
}

// TestWatcherDebounceModelFormallyChecked verifies the debounce policy
// table: a burst never fires (it re-arms instead), a stale timer fire never
// fires and never disarms the newer timer, every edge from armed that fires
// lands on idle, and the table rejects a current-generation fire at idle
// (there is no current generation to fire).
func TestWatcherDebounceModelFormallyChecked(t *testing.T) {
	if err := watchDebounceMachine.Check(fsm.Checker[watchDebounceState, watchDebounceEvent]{
		Alphabet: []watchDebounceEvent{
			evtFsEventQuiet, evtFsEventRapid, evtTrailingFireCurrent,
			evtTrailingFireStale, evtDebounceCancel,
		},
		AllDeclaredReachable: true,
		Edge: func(from watchDebounceState, event watchDebounceEvent, to watchDebounceState) error {
			if event == evtFsEventRapid && watchDebounceFires(from, event) {
				return errors.New("a burst event must re-arm the trailing timer, never fire")
			}
			if event == evtTrailingFireStale && watchDebounceFires(from, event) {
				return errors.New("a stale timer fire must be ignored")
			}
			if event == evtTrailingFireStale && to != from {
				return fmt.Errorf("a stale fire must not disturb the newer timer (%v -> %v)", from, to)
			}
			if watchDebounceFires(from, event) && to != watchDebounceIdle {
				return fmt.Errorf("every firing edge must reset the window (%v --%v--> %v)", from, event, to)
			}
			return nil
		},
	}); err != nil {
		t.Fatalf("watcher debounce machine failed model checking: %v", err)
	}
	if to, ok := watchDebounceMachine.Next(watchDebounceIdle, evtTrailingFireCurrent); ok {
		t.Fatalf("current-generation fire at idle must be rejected by the table, got %v", to)
	}
}

// --- Differential test over event sequences ---

// debounceModel is the spec side of the differential pair. Timestamps are
// extended state; the control state is the machine's. Generation numbers
// stay on the watcher side — the model only needs "a current timer exists".
type debounceModel struct {
	state     watchDebounceState
	lastFire  time.Time
	everFired bool
	debounce  time.Duration
	fires     int // cumulative callback fires, for per-step deltas
}

// event feeds an fs event at now and classifies it exactly as handleChange
// does: at least one debounce past the last fire (or never fired) fires
// immediately; inside the window the trailing timer (re)arms.
func (m *debounceModel) event(now time.Time) bool {
	if !m.everFired || now.Sub(m.lastFire) >= m.debounce {
		m.state = watchDebounceMachine.MustNext(m.state, evtFsEventQuiet)
		m.lastFire, m.everFired = now, true
		m.fires++
		return true
	}
	m.state = watchDebounceMachine.MustNext(m.state, evtFsEventRapid)
	return false
}

// trailingFireCurrent feeds the pending timer's own fire: only an armed
// machine has a current generation, and only it may run the callback.
func (m *debounceModel) trailingFireCurrent(now time.Time) bool {
	if m.state != watchDebounceArmed {
		// No pending timer: a fire arriving here is one of the timer's
		// canceled ancestors racing in — the stale edge.
		m.state = watchDebounceMachine.MustNext(m.state, evtTrailingFireStale)
		return false
	}
	m.state = watchDebounceMachine.MustNext(m.state, evtTrailingFireCurrent)
	m.lastFire = now
	m.fires++
	return true
}

// trailingFireStale feeds a fire whose generation is not the pending one.
func (m *debounceModel) trailingFireStale() bool {
	m.state = watchDebounceMachine.MustNext(m.state, evtTrailingFireStale)
	return false
}

func (m *debounceModel) cancel() {
	m.state = watchDebounceMachine.MustNext(m.state, evtDebounceCancel)
}

// debounceStep is one scripted action the differential test applies to both
// the model and the real watcher. advance is the clock delta for fs events;
// whether an event is quiet or rapid is classified identically by both
// sides from the clock, never from the step's label.
type debounceStep struct {
	kind    watchDebounceEvent
	advance time.Duration
}

// runDifferential drives the real FileWatcher and the model through steps,
// asserting after every step that they agree on whether the callback fired
// and whether a trailing timer is armed.
func runDifferential(t *testing.T, steps []debounceStep) {
	t.Helper()
	root := newGitRoot(t)
	var fires atomic.Int32
	fw, err := NewFileWatcher(func(r string) {
		if r == root {
			fires.Add(1)
		}
	})
	if err != nil {
		t.Fatalf("NewFileWatcher() error = %v", err)
	}
	defer func() {
		fw.Unwatch(root) // stops any pending trailing timer
		_ = fw.Close()
	}()
	// A one-hour debounce keeps the REAL time.AfterFunc timers from firing
	// during the test; timer fires are simulated through fireTrailing with
	// explicit generations, and the nowFunc seam pins the clock the policy
	// reads. Both sides see the same clock and the same debounce.
	fw.debounce = time.Hour
	clock := time.Unix(1_700_000_000, 0)
	fw.nowFunc = func() time.Time { return clock }
	if err := fw.Watch(root); err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	model := debounceModel{state: watchDebounceIdle, debounce: fw.debounce}
	canceled := false
	// lastArmedGen is the generation number the watcher last handed to a
	// real timer — the only number a legitimate timer fire can carry.
	var lastArmedGen uint64

	for i, step := range steps {
		firesBefore := int(fires.Load())
		switch step.kind {
		case evtFsEventQuiet, evtFsEventRapid:
			if canceled {
				continue // Run never dispatches events to an unwatched root
			}
			clock = clock.Add(step.advance)
			model.event(clock)
			fw.handleChange(root)
		case evtTrailingFireCurrent, evtTrailingFireStale:
			fw.mu.Lock()
			armed := fw.pendingTimers[root] != nil
			fw.mu.Unlock()
			var gen uint64
			if armed {
				fw.mu.Lock()
				gen = fw.timerGeneration[root]
				fw.mu.Unlock()
				if step.kind == evtTrailingFireStale {
					// A number from an older epoch (or none at all): it must
					// mismatch the pending timer's generation.
					gen--
				}
			} else if lastArmedGen > 0 {
				// No pending timer, but a timer existed in an earlier epoch:
				// fire the number it carried — the counter has advanced past
				// it (every stop and fire bumps), so this simulates the
				// canceled-ancestor race. A root with no arming ever has no
				// real number to fire, so that step is skipped entirely; and
				// a post-bump counter value is never used, because no real
				// timer ever carries one (numbers are only assigned at
				// arming, and every stop advances past them).
				gen = lastArmedGen
			} else {
				continue
			}
			if step.kind == evtTrailingFireCurrent {
				model.trailingFireCurrent(clock)
			} else {
				model.trailingFireStale()
			}
			fw.fireTrailing(root, gen)
		case evtDebounceCancel:
			canceled = true
			model.cancel()
			fw.Unwatch(root)
		}
		if got, want := int(fires.Load())-firesBefore, model.fires-firesBefore; got != want {
			t.Fatalf("step %d (%v): fire delta mismatch: watcher=%d model=%d", i, step.kind, got, want)
		}
		fw.mu.Lock()
		armed := fw.pendingTimers[root] != nil
		if armed {
			lastArmedGen = fw.timerGeneration[root]
		}
		fw.mu.Unlock()
		if armed != (model.state == watchDebounceArmed) {
			t.Fatalf("step %d (%v): armed mismatch: watcher=%v model=%v", i, step.kind, armed, model.state)
		}
	}
}

// TestWatcherDebounceDifferentialSequences runs hand-picked sequences that
// exercise each policy corner: leading fire, burst re-arm, superseded timer
// race, trailing fire, quiet-after-burst cancellation, and unwatch.
func TestWatcherDebounceDifferentialSequences(t *testing.T) {
	sequences := [][]debounceStep{
		// A lone event fires immediately.
		{{kind: evtFsEventQuiet, advance: 2 * time.Hour}},
		// A burst after the leading fire re-arms; the trailing fire runs.
		{
			{kind: evtFsEventQuiet, advance: 2 * time.Hour},
			{kind: evtFsEventRapid, advance: time.Millisecond},
			{kind: evtTrailingFireCurrent},
		},
		// A superseded generation racing in is ignored; the newer timer
		// still fires.
		{
			{kind: evtFsEventQuiet, advance: 2 * time.Hour},
			{kind: evtFsEventRapid, advance: time.Millisecond},
			{kind: evtFsEventRapid, advance: time.Millisecond},
			{kind: evtTrailingFireStale},
			{kind: evtTrailingFireCurrent},
		},
		// A quiet event during an armed window fires immediately and
		// cancels the pending timer; its late fire is ignored.
		{
			{kind: evtFsEventQuiet, advance: 2 * time.Hour},
			{kind: evtFsEventRapid, advance: time.Millisecond},
			{kind: evtFsEventQuiet, advance: 2 * time.Hour},
			{kind: evtTrailingFireStale},
			{kind: evtFsEventQuiet, advance: 2 * time.Hour},
		},
		// Stale fires at idle (a canceled timer racing in) are ignored.
		{
			{kind: evtFsEventQuiet, advance: 2 * time.Hour},
			{kind: evtFsEventRapid, advance: time.Millisecond},
			{kind: evtTrailingFireCurrent},
			{kind: evtTrailingFireStale},
			{kind: evtFsEventQuiet, advance: 2 * time.Hour},
		},
		// Unwatch cancels the pending timer; its late fire stays silent.
		{
			{kind: evtFsEventQuiet, advance: 2 * time.Hour},
			{kind: evtFsEventRapid, advance: time.Millisecond},
			{kind: evtDebounceCancel},
			{kind: evtTrailingFireStale},
		},
	}
	for i, seq := range sequences {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			runDifferential(t, seq)
		})
	}
}

// TestWatcherDebounceDifferentialRandomized sweeps a seeded-random corpus
// of event sequences through the same differential harness — the model and
// the shipped watcher must agree on every step of every sequence.
func TestWatcherDebounceDifferentialRandomized(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	kinds := []watchDebounceEvent{
		evtFsEventQuiet, evtFsEventRapid, evtTrailingFireCurrent,
		evtTrailingFireStale, evtDebounceCancel,
	}
	for seq := 0; seq < 200; seq++ {
		n := 1 + rng.Intn(24)
		steps := make([]debounceStep, 0, n)
		for i := 0; i < n; i++ {
			kind := kinds[rng.Intn(len(kinds))]
			step := debounceStep{kind: kind}
			if kind == evtFsEventQuiet || kind == evtFsEventRapid {
				// Quiet needs a real clock jump; rapid needs a small one —
				// but the driver classifies by model, so vary freely and
				// let both sides classify identically.
				if rng.Intn(2) == 0 {
					step.advance = time.Hour
				} else {
					step.advance = time.Millisecond
				}
			}
			steps = append(steps, step)
		}
		runDifferential(t, steps)
	}
}
