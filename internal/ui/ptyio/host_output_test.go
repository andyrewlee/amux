package ptyio

import (
	"reflect"
	"sync"
	"testing"

	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/vterm"
)

func counterValue(counters []perf.CounterSnapshot, name string) int64 {
	for _, c := range counters {
		if c.Name == name {
			return c.Value
		}
	}
	return 0
}

func TestAppendOutputNoOverflow(t *testing.T) {
	st := &State{}
	mu := &sync.Mutex{}
	var order []string
	res := st.AppendOutput(mu, []byte("hello"), 1024, OutputHooks{
		OnCarryConsumed: func() { order = append(order, "carry") },
		AfterAppendLocked: func(n int) {
			order = append(order, "append")
			if n != 5 {
				t.Fatalf("appendedLen = %d, want 5", n)
			}
		},
		SeedForTrim:      func() vterm.ParserCarryState { order = append(order, "seed"); return vterm.ParserCarryState{} },
		OnOverflowLocked: func(int, int, int) { order = append(order, "overflow") },
		LogOverflow:      func(int) { order = append(order, "log") },
		DropBytesCounter: "test_out_drop_bytes",
		DropCounter:      "test_out_drop",
	})
	if res.Overflowed {
		t.Fatalf("Overflowed = true, want false when buffer fits")
	}
	if string(res.Data) != "hello" {
		t.Fatalf("res.Data = %q, want %q", res.Data, "hello")
	}
	if res.PrevPendingLen != 0 {
		t.Fatalf("res.PrevPendingLen = %d, want 0", res.PrevPendingLen)
	}
	if string(st.PendingOutput) != "hello" {
		t.Fatalf("PendingOutput = %q, want %q", st.PendingOutput, "hello")
	}
	// Only the always-run append hook fires; no carry, seed, or overflow hooks.
	if !reflect.DeepEqual(order, []string{"append"}) {
		t.Fatalf("hook order = %v, want [append]", order)
	}
}

func TestAppendOutputOverflowTrimsAndCountsAndOrders(t *testing.T) {
	restore := perf.EnableForTest()
	defer restore()
	perf.Snapshot() // clear

	st := &State{}
	mu := &sync.Mutex{}
	var order []string
	var gotOverflow, gotRetainedStart, gotPrevPendingLen int
	res := st.AppendOutput(mu, []byte("abcdefgh"), 4, OutputHooks{
		OnCarryConsumed:   func() { order = append(order, "carry") },
		AfterAppendLocked: func(int) { order = append(order, "append") },
		SeedForTrim:       func() vterm.ParserCarryState { order = append(order, "seed"); return vterm.ParserCarryState{} },
		OnOverflowLocked: func(overflow, retainedStart, prevPendingLen int) {
			order = append(order, "overflow")
			gotOverflow, gotRetainedStart, gotPrevPendingLen = overflow, retainedStart, prevPendingLen
		},
		LogOverflow:      func(int) { order = append(order, "log") },
		DropBytesCounter: "test_out_drop_bytes",
		DropCounter:      "test_out_drop",
	})
	if !res.Overflowed {
		t.Fatalf("Overflowed = false, want true when buffer exceeds cap")
	}
	// Plain ASCII with a zero seed cuts at the exact overflow boundary.
	if string(st.PendingOutput) != "efgh" {
		t.Fatalf("PendingOutput = %q, want %q", st.PendingOutput, "efgh")
	}
	if res.RetainedStart != 4 || gotRetainedStart != 4 {
		t.Fatalf("retainedStart res=%d hook=%d, want 4", res.RetainedStart, gotRetainedStart)
	}
	if gotOverflow != 4 {
		t.Fatalf("overflow hook arg = %d, want 4", gotOverflow)
	}
	if gotPrevPendingLen != 0 {
		t.Fatalf("prevPendingLen hook arg = %d, want 0", gotPrevPendingLen)
	}
	// Append runs before the seed/overflow trim; overflow bookkeeping before the log.
	if !reflect.DeepEqual(order, []string{"append", "seed", "overflow", "log"}) {
		t.Fatalf("hook order = %v, want [append seed overflow log]", order)
	}
	_, counters := perf.Snapshot()
	if v := counterValue(counters, "test_out_drop_bytes"); v != 4 {
		t.Fatalf("test_out_drop_bytes = %d, want 4", v)
	}
	if v := counterValue(counters, "test_out_drop"); v != 1 {
		t.Fatalf("test_out_drop = %d, want 1", v)
	}
}

func TestAppendOutputConsumesCarry(t *testing.T) {
	// A non-zero OverflowTrimCarry marks an unfinished cut; the next chunk must
	// run OnCarryConsumed and advance past the carried prefix.
	st := &State{OverflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryOSC}}
	mu := &sync.Mutex{}
	carried := false
	res := st.AppendOutput(mu, []byte("\x1b\\rest"), 1024, OutputHooks{
		OnCarryConsumed: func() { carried = true },
	})
	if !carried {
		t.Fatalf("OnCarryConsumed not invoked despite pending carry")
	}
	if res.Overflowed {
		t.Fatalf("Overflowed = true, want false")
	}
	if st.OverflowTrimCarry != (vterm.ParserCarryState{}) {
		t.Fatalf("OverflowTrimCarry not cleared after consuming carry: %+v", st.OverflowTrimCarry)
	}
}

func TestAppendOutputNilHooksSafe(t *testing.T) {
	st := &State{}
	mu := &sync.Mutex{}
	// Overflow with all hooks nil and empty counter names must not panic.
	res := st.AppendOutput(mu, []byte("abcdefgh"), 4, OutputHooks{})
	if !res.Overflowed {
		t.Fatalf("Overflowed = false, want true")
	}
	if string(st.PendingOutput) != "efgh" {
		t.Fatalf("PendingOutput = %q, want %q", st.PendingOutput, "efgh")
	}
}
