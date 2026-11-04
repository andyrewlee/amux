package process

import (
	"errors"
	"os/exec"
	"testing"
)

// coordSeed builds a coordinator whose key sits in the given protocol state,
// returning the coordinator, the key, and the state's setup ticket (nil when
// the slot is empty). Every seed is reached through the coordinator's own
// methods, so the derivation is exercised against real field combos.
func coordSeed(t *testing.T, s coordState) (*lifecycleCoordinator, string, *lifecycleTicket) {
	t.Helper()
	const key = "ws"
	c := newLifecycleCoordinator()
	var ticket *lifecycleTicket
	mustTeardown := func() {
		if _, err := c.beginTeardown(key); err != nil {
			t.Fatalf("seed %v: beginTeardown: %v", s, err)
		}
	}
	switch s {
	case coordIdle:
		// Fresh coordinator: no record yet.
	case coordSetupInflight:
		var err error
		ticket, err = c.admitSetup(key)
		if err != nil {
			t.Fatalf("seed setup-inflight: admitSetup: %v", err)
		}
	case coordSetupStale:
		var err error
		ticket, err = c.admitSetup(key)
		if err != nil {
			t.Fatalf("seed setup-stale: admitSetup: %v", err)
		}
		mustTeardown()
		c.releaseTeardown(key)
	case coordTeardownGated:
		mustTeardown()
	case coordTeardownGatedStale:
		var err error
		ticket, err = c.admitSetup(key)
		if err != nil {
			t.Fatalf("seed teardown-gated-stale: admitSetup: %v", err)
		}
		mustTeardown()
	}
	if got := c.coordStateOf(key); got != s {
		t.Fatalf("seed %v: derived state is %v", s, got)
	}
	return c, key, ticket
}

// ticketDoneClosed reports whether the ticket's drain channel is closed.
func ticketDoneClosed(t *lifecycleTicket) bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

// TestCoordinatorMachineCheck runs the formal verification of the admission
// table and its progress shape in plain go test.
func TestCoordinatorMachineCheck(t *testing.T) {
	if err := coordMachineCheck(); err != nil {
		t.Error(err)
	}
	if err := coordProgressHolds(); err != nil {
		t.Error(err)
	}
}

// differentialAdmitSetup checks admitSetup against the table: busy vs
// teardown-gated refusals, and that an admitted ticket is current.
func differentialAdmitSetup(t *testing.T, c *lifecycleCoordinator, key string, seed coordState, ok bool) {
	t.Helper()
	got, err := c.admitSetup(key)
	gated := seed == coordTeardownGated || seed == coordTeardownGatedStale
	switch {
	case ok:
		if err != nil {
			t.Fatalf("table admits, coordinator refused: %v", err)
		}
		if !c.ticketCurrent(key, got) {
			t.Fatal("admitted ticket must be current")
		}
	case gated:
		if !errors.Is(err, ErrWorkspaceTeardown) {
			t.Fatalf("want ErrWorkspaceTeardown, got %v", err)
		}
	default:
		if !errors.Is(err, ErrSetupBusy) {
			t.Fatalf("want ErrSetupBusy, got %v", err)
		}
	}
}

// differentialTicketCurrent checks revalidation and command registration
// accept exactly where the table says.
func differentialTicketCurrent(t *testing.T, c *lifecycleCoordinator, key string, ticket *lifecycleTicket, ok bool) {
	t.Helper()
	if got := c.ticketCurrent(key, ticket); got != ok {
		t.Fatalf("ticketCurrent = %v, table predicts %v", got, ok)
	}
	if got := c.registerCmd(key, ticket, &exec.Cmd{}); got != ok {
		t.Fatalf("registerCmd = %v, table predicts %v", got, ok)
	}
}

// differentialBeginTeardown checks teardown admission, snapshot ticket
// identity, and that a torn-down ticket stops revalidating.
func differentialBeginTeardown(t *testing.T, c *lifecycleCoordinator, key string, ticket *lifecycleTicket, ok bool) {
	t.Helper()
	snap, err := c.beginTeardown(key)
	if ok {
		if err != nil {
			t.Fatalf("table admits teardown, coordinator refused: %v", err)
		}
		if snap.ticket != ticket {
			t.Fatalf("snapshot ticket = %v, want the state's ticket %v", snap.ticket, ticket)
		}
		if ticket != nil && c.ticketCurrent(key, ticket) {
			t.Fatal("a torn-down ticket must no longer revalidate as current")
		}
	} else if !errors.Is(err, ErrWorkspaceTeardown) {
		t.Fatalf("want ErrWorkspaceTeardown, got %v", err)
	}
}

// differentialFinishRemoved checks the removal guard's idempotence and the
// clean post-removal admission state.
func differentialFinishRemoved(t *testing.T, c *lifecycleCoordinator, key string) {
	t.Helper()
	g := &TeardownGuard{coord: c, key: key}
	g.Finish(true)
	g.Finish(true) // idempotent: the second call must be a no-op
	if got := c.coordStateOf(key); got != coordIdle {
		t.Fatalf("after Finish(true) twice, state = %v, want idle", got)
	}
	if _, err := c.admitSetup(key); !errors.Is(err, ErrSetupBusy) && err != nil {
		t.Fatalf("post-removal admission should be clean, got %v", err)
	}
}

// differentialAdmitOnDone checks on-done admission, drain closure, and the
// refused-admission contract.
func differentialAdmitOnDone(t *testing.T, c *lifecycleCoordinator, key string, ok bool) {
	t.Helper()
	p, err := c.admitOnDone(key, &exec.Cmd{})
	if ok {
		if err != nil {
			t.Fatalf("table admits, coordinator refused: %v", err)
		}
		c.finishOnDone(key, p)
		select {
		case <-p.done:
		default:
			t.Fatal("finishOnDone must close the proc's drain channel")
		}
	} else if !errors.Is(err, ErrWorkspaceTeardown) {
		t.Fatalf("want ErrWorkspaceTeardown, got %v", err)
	} else if p != nil {
		t.Fatal("a refused admission must return no proc")
	}
}

// differentialStopAll checks the global stop: every key emptied, all three
// admission entry points refusing, and ticket channels left for their
// owning goroutines to close.
func differentialStopAll(t *testing.T, c *lifecycleCoordinator, key string, ticket *lifecycleTicket) {
	t.Helper()
	c.stopAllSnapshots()
	if got := c.coordStateOf(key); got != coordIdle {
		t.Fatalf("StopAll must empty every key, got %v", got)
	}
	if _, err := c.admitSetup(key); !errors.Is(err, ErrRunnerStopped) {
		t.Fatalf("post-StopAll admitSetup: want ErrRunnerStopped, got %v", err)
	}
	if _, err := c.admitOnDone(key, &exec.Cmd{}); !errors.Is(err, ErrRunnerStopped) {
		t.Fatalf("post-StopAll admitOnDone: want ErrRunnerStopped, got %v", err)
	}
	if err := c.checkAdmission(key); !errors.Is(err, ErrRunnerStopped) {
		t.Fatalf("post-StopAll checkAdmission: want ErrRunnerStopped, got %v", err)
	}
	if ticket != nil && ticketDoneClosed(ticket) {
		t.Fatalf("StopAll must not close ticket channels itself; the owning goroutine's finishSetup does")
	}
}

// TestCoordinatorDifferential walks a representative cross-product of
// protocol states x events and asserts the real coordinator's behavior
// matches the checked table's prediction: which admissions are refused and
// with which error, when ticket revalidation accepts, where teardown may
// begin, and how the guard and StopAll behave.
func TestCoordinatorDifferential(t *testing.T) {
	for _, seed := range []coordState{coordIdle, coordSetupInflight, coordSetupStale, coordTeardownGated, coordTeardownGatedStale} {
		for _, evt := range coordMachine.Events() {
			predicted, ok := coordMachine.Next(seed, evt)
			t.Run(seed.String()+"/"+evt.String(), func(t *testing.T) {
				c, key, ticket := coordSeed(t, seed)

				switch evt {
				case evtAdmitSetup:
					differentialAdmitSetup(t, c, key, seed, ok)

				case evtFinishSetup:
					if ticket == nil {
						// No ticket in the slot: finishSetup on an absent or
						// foreign ticket is a state-preserving no-op.
						return
					}
					c.finishSetup(key, ticket)
					if !ticketDoneClosed(ticket) {
						t.Fatal("finishSetup must close the ticket's drain channel")
					}

				case evtTicketCurrent:
					if ticket == nil {
						if ok {
							t.Fatal("table accepts revalidation without a ticket")
						}
						return
					}
					differentialTicketCurrent(t, c, key, ticket, ok)

				case evtBeginTeardown:
					differentialBeginTeardown(t, c, key, ticket, ok)

				case evtReleaseTeardown:
					c.releaseTeardown(key)

				case evtFinishRemoved:
					if !ok {
						return // no guard can exist outside a gated state
					}
					differentialFinishRemoved(t, c, key)

				case evtForget:
					c.forget(key)
					if ok {
						c.mu.Lock()
						_, present := c.states[key]
						c.mu.Unlock()
						if present {
							t.Fatal("forget from idle must delete the record")
						}
					}

				case evtAdmitOnDone:
					differentialAdmitOnDone(t, c, key, ok)

				case evtFinishOnDone:
					// Draining is exercised above; the table pins it as a
					// no-op wherever admission is possible.
					if !ok {
						return
					}
					p, err := c.admitOnDone(key, &exec.Cmd{})
					if err != nil {
						t.Fatalf("admitOnDone for finish round-trip: %v", err)
					}
					c.finishOnDone(key, p)

				case evtStopAll:
					differentialStopAll(t, c, key, ticket)
				}

				// State-only events must land exactly where the table says
				// (refused events leave the state untouched).
				if evt != evtStopAll && evt != evtFinishRemoved {
					differentialAssertState(t, c, key, seed, evt, predicted, ok)
				}
			})
		}
	}
}

// differentialAssertState checks the coordinator's landed state against the
// table's prediction, folding the extended-state exceptions: on-done sets
// and revalidation never move the control state, and a fresh admission
// always lands setup-inflight.
func differentialAssertState(t *testing.T, c *lifecycleCoordinator, key string, seed coordState, evt coordEvent, predicted coordState, ok bool) {
	t.Helper()
	want := predicted
	if !ok {
		want = seed
	}
	if evt == evtAdmitOnDone || evt == evtFinishOnDone || evt == evtTicketCurrent {
		want = seed // on-done sets and revalidation are extended state
	}
	if evt == evtAdmitSetup && ok {
		want = coordSetupInflight
	}
	if got := c.coordStateOf(key); got != want {
		t.Fatalf("after %v: state = %v, table predicts %v", evt, got, want)
	}
}

// TestCoordinatorTeardownSticky proves the gate's stickiness end to end:
// from a gated state every admission refuses until the guard releases, and
// Finish(false) keeps the superseded generation so pre-teardown tickets stay
// stale forever.
func TestCoordinatorTeardownSticky(t *testing.T) {
	const key = "ws"
	c := newLifecycleCoordinator()
	ticket, err := c.admitSetup(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.beginTeardown(key); err != nil {
		t.Fatal(err)
	}
	// Stickiness: everything refuses while the gate is held.
	if _, err := c.admitSetup(key); !errors.Is(err, ErrWorkspaceTeardown) {
		t.Fatalf("admitSetup under gate: %v", err)
	}
	if _, err := c.admitOnDone(key, &exec.Cmd{}); !errors.Is(err, ErrWorkspaceTeardown) {
		t.Fatalf("admitOnDone under gate: %v", err)
	}
	if _, err := c.beginTeardown(key); !errors.Is(err, ErrWorkspaceTeardown) {
		t.Fatalf("re-entrant beginTeardown: %v", err)
	}
	if c.ticketCurrent(key, ticket) {
		t.Fatal("superseded ticket must not revalidate")
	}
	if err := c.checkAdmission(key); !errors.Is(err, ErrWorkspaceTeardown) {
		t.Fatalf("checkAdmission under gate: %v", err)
	}
	// The guard owns the only release; it is idempotent and keeps the bump.
	g := &TeardownGuard{coord: c, key: key}
	if err := c.checkArchiveAdmission(key, nil); !errors.Is(err, ErrWorkspaceTeardown) {
		t.Fatalf("foreign archive under gate: %v", err)
	}
	if err := c.checkArchiveAdmission(key, g); err != nil {
		t.Fatalf("owning guard archive under gate: %v", err)
	}
	g.Finish(false)
	g.Finish(false) // second release is a no-op
	if got := c.coordStateOf(key); got != coordSetupStale {
		t.Fatalf("after Finish(false): state = %v, want setup-stale", got)
	}
	if c.ticketCurrent(key, ticket) {
		t.Fatal("pre-teardown ticket must stay stale after release")
	}
	if _, err := c.admitSetup(key); !errors.Is(err, ErrSetupBusy) {
		t.Fatalf("stale ticket must hold the slot until finishSetup: %v", err)
	}
	// The stale ticket can still finish — no dead end.
	c.finishSetup(key, ticket)
	if !ticketDoneClosed(ticket) {
		t.Fatal("finishSetup must close the drain channel")
	}
	if got := c.coordStateOf(key); got != coordIdle {
		t.Fatalf("after finishSetup: state = %v, want idle", got)
	}
}
