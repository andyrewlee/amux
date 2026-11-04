package coalesce

import "testing"

type want struct {
	full bool
}

func mergeWant(a, b want) want { return want{full: a.full || b.full} }

func TestMachineFormallyChecked(t *testing.T) {
	if err := Check(); err != nil {
		t.Fatalf("coalescer machine failed model checking: %v", err)
	}
}

func TestAdmitStartsOnlyWhenIdle(t *testing.T) {
	e := New(mergeWant)
	if start, _ := e.Admit(want{full: false}); !start || e.State() != InFlight {
		t.Fatalf("idle admit must start: start=%v state=%v", start, e.State())
	}
	if start, _ := e.Admit(want{full: true}); start || e.State() != InFlightPending {
		t.Fatalf("mid-flight admit must coalesce: start=%v state=%v", start, e.State())
	}
	if start, _ := e.Admit(want{full: false}); start || e.State() != InFlightPending {
		t.Fatalf("second mid-flight admit must stay pending: start=%v state=%v", start, e.State())
	}
}

func TestCompleteReturnsMergedObligation(t *testing.T) {
	e := New(mergeWant)
	e.Admit(want{full: false}) // start
	e.Admit(want{full: false}) // pending
	e.Admit(want{full: true})  // pending, upgraded
	rerun, payload := e.Complete()
	if !rerun || !payload.full {
		t.Fatalf("follow-up owed with merged payload: rerun=%v payload=%+v", rerun, payload)
	}
	if e.State() != Idle {
		t.Fatalf("completion must land on idle, got %v", e.State())
	}
	if rerun, _ := e.Complete(); rerun {
		t.Fatal("second completion owes nothing")
	}
}

func TestPanicResetKeepsObligation(t *testing.T) {
	e := New(mergeWant)
	e.Admit(want{full: true})
	e.Admit(want{full: true})
	e.PanicReset()
	if e.State() != IdlePending {
		t.Fatalf("panic reset with pending must reach idle+pending, got %v", e.State())
	}
	rerun, payload := e.Complete() // the still-running producer's result
	if !rerun || !payload.full {
		t.Fatalf("obligation must survive the panic reset: rerun=%v payload=%+v", rerun, payload)
	}
	if e.State() != Idle {
		t.Fatalf("completion must land on idle, got %v", e.State())
	}
}

func TestIdlePendingAdmissionSubsumesObligation(t *testing.T) {
	e := New(mergeWant)
	e.Admit(want{full: true})
	e.Admit(want{full: true})
	e.PanicReset() // -> IdlePending with obligation
	start, payload := e.Admit(want{full: false})
	if !start || !payload.full {
		t.Fatalf("admission from idle+pending must start with merged payload: start=%v payload=%+v", start, payload)
	}
	if e.State() != InFlight {
		t.Fatalf("state after subsuming admission: %v", e.State())
	}
	if rerun, _ := e.Complete(); rerun {
		t.Fatal("starting subsumes the obligation; completion must owe nothing")
	}
}
