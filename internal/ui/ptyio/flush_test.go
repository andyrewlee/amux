package ptyio

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/vterm"
)

func TestNoteOverflowDropLocked(t *testing.T) {
	t.Run("first call always logs and reports accumulated total", func(t *testing.T) {
		st := &State{}
		logNow, total := st.NoteOverflowDropLocked(128)
		if !logNow {
			t.Fatalf("logNow = false, want true on first (zero LastOverflowLogAt) call")
		}
		if total != 128 {
			t.Fatalf("total = %d, want 128", total)
		}
		// Counter is reset and the throttle timestamp is now set.
		if st.OverflowDroppedSinceLog != 0 {
			t.Fatalf("OverflowDroppedSinceLog = %d, want 0 after reporting", st.OverflowDroppedSinceLog)
		}
		if st.LastOverflowLogAt.IsZero() {
			t.Fatalf("LastOverflowLogAt should be set after logging")
		}
	})

	t.Run("within throttle window accumulates without logging", func(t *testing.T) {
		st := &State{
			LastOverflowLogAt: time.Now(),
		}
		logNow, total := st.NoteOverflowDropLocked(10)
		if logNow {
			t.Fatalf("logNow = true, want false within throttle window")
		}
		if total != 0 {
			t.Fatalf("total = %d, want 0 when not logging", total)
		}
		if st.OverflowDroppedSinceLog != 10 {
			t.Fatalf("OverflowDroppedSinceLog = %d, want 10 accumulated", st.OverflowDroppedSinceLog)
		}

		// A second suppressed drop keeps accumulating.
		logNow, total = st.NoteOverflowDropLocked(5)
		if logNow {
			t.Fatalf("logNow = true on second suppressed call, want false")
		}
		if total != 0 {
			t.Fatalf("total = %d, want 0 on second suppressed call", total)
		}
		if st.OverflowDroppedSinceLog != 15 {
			t.Fatalf("OverflowDroppedSinceLog = %d, want 15 accumulated", st.OverflowDroppedSinceLog)
		}
	})

	t.Run("after throttle window elapses, aggregated total is reported and reset", func(t *testing.T) {
		st := &State{
			LastOverflowLogAt:       time.Now().Add(-2 * OverflowLogThrottle),
			OverflowDroppedSinceLog: 40,
		}
		logNow, total := st.NoteOverflowDropLocked(60)
		if !logNow {
			t.Fatalf("logNow = false, want true after throttle window elapsed")
		}
		if total != 100 {
			t.Fatalf("total = %d, want 100 (40 carried + 60 new)", total)
		}
		if st.OverflowDroppedSinceLog != 0 {
			t.Fatalf("OverflowDroppedSinceLog = %d, want 0 after reporting", st.OverflowDroppedSinceLog)
		}
	})

	t.Run("exactly at throttle boundary logs", func(t *testing.T) {
		st := &State{
			LastOverflowLogAt: time.Now().Add(-OverflowLogThrottle),
		}
		logNow, total := st.NoteOverflowDropLocked(7)
		if !logNow {
			t.Fatalf("logNow = false, want true at exactly the throttle boundary")
		}
		if total != 7 {
			t.Fatalf("total = %d, want 7", total)
		}
	})

	t.Run("zero dropped bytes on first call still logs with zero total", func(t *testing.T) {
		st := &State{}
		logNow, total := st.NoteOverflowDropLocked(0)
		if !logNow {
			t.Fatalf("logNow = false, want true on first call even with zero drop")
		}
		if total != 0 {
			t.Fatalf("total = %d, want 0", total)
		}
	})
}

func TestConsumeOverflowCarryLocked(t *testing.T) {
	t.Run("no pending carry returns data unchanged and false", func(t *testing.T) {
		st := &State{}
		in := []byte("hello world")
		got, consumed := st.ConsumeOverflowCarryLocked(in)
		if consumed {
			t.Fatalf("consumed = true, want false when no carry pending")
		}
		if string(got) != "hello world" {
			t.Fatalf("got %q, want %q", got, "hello world")
		}
		if st.OverflowTrimCarry != (vterm.ParserCarryState{}) {
			t.Fatalf("carry should stay zero, got %+v", st.OverflowTrimCarry)
		}
	})

	t.Run("pending CSI carry trims the unsafe prefix and clears the carry", func(t *testing.T) {
		st := &State{OverflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryCSI}}
		// Leading "31m" completes the carried CSI sequence; "visible" follows.
		got, consumed := st.ConsumeOverflowCarryLocked([]byte("31mvisible"))
		if !consumed {
			t.Fatalf("consumed = false, want true when carry is pending")
		}
		if string(got) != "visible" {
			t.Fatalf("got %q, want %q", got, "visible")
		}
		if st.OverflowTrimCarry != (vterm.ParserCarryState{}) {
			t.Fatalf("carry should be cleared after the sequence completes, got %+v", st.OverflowTrimCarry)
		}
	})

	t.Run("pending carry with no boundary keeps the carry and drops everything", func(t *testing.T) {
		st := &State{OverflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryOSC}}
		// The whole chunk is still inside the carried OSC sequence: nothing is safe.
		got, consumed := st.ConsumeOverflowCarryLocked([]byte("title"))
		if !consumed {
			t.Fatalf("consumed = false, want true when carry is pending")
		}
		if got != nil {
			t.Fatalf("got %q, want nil when no safe boundary remains", got)
		}
		if st.OverflowTrimCarry != (vterm.ParserCarryState{Mode: vterm.ParserCarryOSC}) {
			t.Fatalf("carry should persist when unresolved, got %+v", st.OverflowTrimCarry)
		}
	})

	t.Run("pending carry with empty data returns empty and preserves carry", func(t *testing.T) {
		st := &State{OverflowTrimCarry: vterm.ParserCarryState{Mode: vterm.ParserCarryEscape}}
		got, consumed := st.ConsumeOverflowCarryLocked([]byte{})
		if !consumed {
			t.Fatalf("consumed = false, want true when carry is pending")
		}
		if len(got) != 0 {
			t.Fatalf("got %q, want empty", got)
		}
		// TrimPTYOverflowPrefix returns the seed unchanged for empty data.
		if st.OverflowTrimCarry != (vterm.ParserCarryState{Mode: vterm.ParserCarryEscape}) {
			t.Fatalf("carry should be preserved for empty data, got %+v", st.OverflowTrimCarry)
		}
	})
}

func TestTrimOverflow(t *testing.T) {
	t.Run("drops the overflow prefix at a safe text boundary", func(t *testing.T) {
		pending := []byte("hello world")
		// maxBuffered = 5 means overflow of len-5 = 6 bytes to drop from the front.
		retained, carry, dropped := TrimOverflow(pending, 5, vterm.ParserCarryState{})
		if string(retained) != "world" {
			t.Fatalf("retained = %q, want %q", retained, "world")
		}
		if dropped != 6 {
			t.Fatalf("dropped = %d, want 6", dropped)
		}
		if carry != (vterm.ParserCarryState{}) {
			t.Fatalf("carry = %+v, want zero at a clean text boundary", carry)
		}
	})

	t.Run("returns a fresh copy that does not alias the input", func(t *testing.T) {
		pending := []byte("ABCDEFGH")
		retained, _, _ := TrimOverflow(pending, 4, vterm.ParserCarryState{})
		if string(retained) != "EFGH" {
			t.Fatalf("retained = %q, want %q", retained, "EFGH")
		}
		// Mutating the source must not affect the returned copy.
		for i := range pending {
			pending[i] = 'x'
		}
		if string(retained) != "EFGH" {
			t.Fatalf("retained aliases input: got %q after mutating source", retained)
		}
	})

	t.Run("advances past a truncated CSI sequence tail", func(t *testing.T) {
		// maxBuffered keeps the last len-overflow bytes; choose overflow so the
		// cut lands inside the CSI sequence and gets advanced to "xyz".
		pending := []byte("abc\x1b[>1;10;0cxyz")
		overflow := len("abc\x1b[>")
		maxBuffered := len(pending) - overflow
		retained, _, dropped := TrimOverflow(pending, maxBuffered, vterm.ParserCarryState{})
		if string(retained) != "xyz" {
			t.Fatalf("retained = %q, want %q", retained, "xyz")
		}
		if dropped != len(pending)-len("xyz") {
			t.Fatalf("dropped = %d, want %d", dropped, len(pending)-len("xyz"))
		}
	})

	t.Run("no overflow (negative) drops nothing and keeps all bytes", func(t *testing.T) {
		pending := []byte("short")
		// maxBuffered larger than len -> overflow negative -> drop clamped to 0.
		retained, carry, dropped := TrimOverflow(pending, 100, vterm.ParserCarryState{})
		if string(retained) != "short" {
			t.Fatalf("retained = %q, want %q", retained, "short")
		}
		if dropped != 0 {
			t.Fatalf("dropped = %d, want 0", dropped)
		}
		if carry != (vterm.ParserCarryState{}) {
			t.Fatalf("carry = %+v, want zero", carry)
		}
	})

	t.Run("empty pending returns empty retained and no drop", func(t *testing.T) {
		retained, carry, dropped := TrimOverflow(nil, 0, vterm.ParserCarryState{})
		if len(retained) != 0 {
			t.Fatalf("retained = %q, want empty", retained)
		}
		if dropped != 0 {
			t.Fatalf("dropped = %d, want 0", dropped)
		}
		if carry != (vterm.ParserCarryState{}) {
			t.Fatalf("carry = %+v, want zero", carry)
		}
	})

	t.Run("dropping everything leaves an unresolved carry and empty retained", func(t *testing.T) {
		// Whole buffer is inside an open OSC sequence seeded by carry; no safe
		// boundary so everything is dropped and the carry persists.
		pending := []byte("title")
		retained, carry, dropped := TrimOverflow(pending, 0, vterm.ParserCarryState{Mode: vterm.ParserCarryOSC})
		if len(retained) != 0 {
			t.Fatalf("retained = %q, want empty", retained)
		}
		if dropped != len(pending) {
			t.Fatalf("dropped = %d, want %d", dropped, len(pending))
		}
		if carry != (vterm.ParserCarryState{Mode: vterm.ParserCarryOSC}) {
			t.Fatalf("carry = %+v, want OSC carry preserved", carry)
		}
	})
}

func TestFlushDelay(t *testing.T) {
	const (
		quiet       = 20 * time.Millisecond
		maxInterval = 200 * time.Millisecond
	)
	base := time.Now()

	t.Run("defers while output is still arriving and flush is young", func(t *testing.T) {
		st := &State{
			LastOutputAt:      base, // quietFor = 5ms < quiet
			FlushPendingSince: base, // pendingFor = 5ms < maxInterval
		}
		now := base.Add(5 * time.Millisecond)
		delay, shouldDefer := st.FlushDelay(now, quiet, maxInterval)
		if !shouldDefer {
			t.Fatalf("defer = false, want true while still receiving output")
		}
		if want := quiet - 5*time.Millisecond; delay != want {
			t.Fatalf("delay = %v, want %v (remaining quiet period)", delay, want)
		}
	})

	t.Run("clamps a sub-millisecond remaining delay up to 1ms", func(t *testing.T) {
		st := &State{
			LastOutputAt:      base,
			FlushPendingSince: base,
		}
		// quietFor = quiet-500µs, so remaining = 500µs which clamps up to 1ms.
		now := base.Add(quiet - 500*time.Microsecond)
		delay, shouldDefer := st.FlushDelay(now, quiet, maxInterval)
		if !shouldDefer {
			t.Fatalf("defer = false, want true (still within quiet period)")
		}
		if delay != time.Millisecond {
			t.Fatalf("delay = %v, want 1ms (clamped minimum)", delay)
		}
	})

	t.Run("flushes once the quiet period has fully elapsed", func(t *testing.T) {
		st := &State{
			LastOutputAt:      base,
			FlushPendingSince: base,
		}
		now := base.Add(quiet) // quietFor == quiet, not < quiet
		delay, shouldDefer := st.FlushDelay(now, quiet, maxInterval)
		if shouldDefer {
			t.Fatalf("defer = true, want false once quiet period elapsed")
		}
		if delay != 0 {
			t.Fatalf("delay = %v, want 0 when flushing", delay)
		}
	})

	t.Run("flushes despite ongoing output once maxInterval is exceeded", func(t *testing.T) {
		st := &State{
			LastOutputAt:      base.Add(10 * time.Millisecond), // recent output: quietFor small
			FlushPendingSince: base,                            // pending a long time
		}
		now := base.Add(maxInterval + time.Millisecond) // pendingFor >= maxInterval
		delay, shouldDefer := st.FlushDelay(now, quiet, maxInterval)
		if shouldDefer {
			t.Fatalf("defer = true, want false once maxInterval exceeded")
		}
		if delay != 0 {
			t.Fatalf("delay = %v, want 0", delay)
		}
	})

	t.Run("zero FlushPendingSince treats pendingFor as zero", func(t *testing.T) {
		st := &State{
			LastOutputAt: base, // quietFor small -> still deferring
			// FlushPendingSince left zero -> pendingFor = 0 < maxInterval
		}
		now := base.Add(2 * time.Millisecond)
		delay, shouldDefer := st.FlushDelay(now, quiet, maxInterval)
		if !shouldDefer {
			t.Fatalf("defer = false, want true (quiet period not elapsed, no pending age)")
		}
		if want := quiet - 2*time.Millisecond; delay != want {
			t.Fatalf("delay = %v, want %v", delay, want)
		}
	})
}
