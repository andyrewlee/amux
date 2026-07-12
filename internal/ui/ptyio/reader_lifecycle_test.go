package ptyio

import (
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/testutil"
)

func TestStopReaderClearsStateAndSignalsCancel(t *testing.T) {
	var mu sync.Mutex
	cancel := make(chan struct{})
	st := &State{
		ReaderActive: true,
		ReaderCancel: cancel,
		MsgCh:        make(chan tea.Msg),
		Heartbeat:    time.Now().UnixNano(),
	}

	st.StopReader(&mu)

	// Cancel channel is closed so the reader can observe it.
	select {
	case <-cancel:
	default:
		t.Fatal("ReaderCancel was not closed by StopReader")
	}

	mu.Lock()
	defer mu.Unlock()
	if st.ReaderActive {
		t.Fatal("ReaderActive = true after StopReader, want false")
	}
	if st.ReaderCancel != nil {
		t.Fatal("ReaderCancel != nil after StopReader, want nil")
	}
	if st.MsgCh != nil {
		t.Fatal("MsgCh != nil after StopReader, want nil")
	}
	if hb := atomic.LoadInt64(&st.Heartbeat); hb != 0 {
		t.Fatalf("Heartbeat = %d after StopReader, want 0", hb)
	}
}

func TestStopReaderWithNilCancelIsSafe(t *testing.T) {
	var mu sync.Mutex
	st := &State{ReaderActive: true, ReaderCancel: nil, Heartbeat: 42}

	// No cancel channel to close: StopReader must not panic and still clear state.
	st.StopReader(&mu)

	mu.Lock()
	defer mu.Unlock()
	if st.ReaderActive {
		t.Fatal("ReaderActive = true after StopReader with nil cancel, want false")
	}
	if hb := atomic.LoadInt64(&st.Heartbeat); hb != 0 {
		t.Fatalf("Heartbeat = %d, want 0", hb)
	}
}

func TestMarkReaderStopped(t *testing.T) {
	var mu sync.Mutex
	cancel := make(chan struct{})
	st := &State{
		ReaderActive: true,
		ReaderCancel: cancel,
		MsgCh:        make(chan tea.Msg),
		Heartbeat:    time.Now().UnixNano(),
		ReaderGen:    1,
	}

	st.MarkReaderStopped(&mu, 1)

	mu.Lock()
	defer mu.Unlock()
	if st.ReaderActive {
		t.Fatal("ReaderActive = true after MarkReaderStopped, want false")
	}
	if st.MsgCh != nil {
		t.Fatal("MsgCh != nil after MarkReaderStopped, want nil")
	}
	if hb := atomic.LoadInt64(&st.Heartbeat); hb != 0 {
		t.Fatalf("Heartbeat = %d after MarkReaderStopped, want 0", hb)
	}
	// ReaderCancel is intentionally left intact for the next StartReader to close.
	if st.ReaderCancel != cancel {
		t.Fatal("ReaderCancel was cleared by MarkReaderStopped, want it left in place")
	}
	// The channel must still be open (not closed by MarkReaderStopped).
	select {
	case <-st.ReaderCancel:
		t.Fatal("ReaderCancel was closed by MarkReaderStopped, want left open")
	default:
	}
}

func TestMarkReaderStoppedStaleGenIsNoOp(t *testing.T) {
	var mu sync.Mutex
	st := &State{
		ReaderActive: true,
		ReaderCancel: make(chan struct{}),
		MsgCh:        make(chan tea.Msg),
		Heartbeat:    time.Now().UnixNano(),
		ReaderGen:    2,
	}
	st.MarkReaderStopped(&mu, 1) // stale generation

	mu.Lock()
	defer mu.Unlock()
	if !st.ReaderActive {
		t.Fatal("stale MarkReaderStopped cleared ReaderActive")
	}
	if st.MsgCh == nil {
		t.Fatal("stale MarkReaderStopped cleared MsgCh")
	}
	if hb := atomic.LoadInt64(&st.Heartbeat); hb == 0 {
		t.Fatal("stale MarkReaderStopped zeroed Heartbeat")
	}
}

func TestReaderStalled(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name         string
		active       bool
		heartbeat    int64
		stallTimeout time.Duration
		want         bool
	}{
		{
			name:         "inactive reader is never stalled",
			active:       false,
			heartbeat:    now.Add(-time.Hour).UnixNano(),
			stallTimeout: time.Second,
			want:         false,
		},
		{
			name:         "active with zero heartbeat is not stalled",
			active:       true,
			heartbeat:    0,
			stallTimeout: time.Second,
			want:         false,
		},
		{
			name:         "active with recent heartbeat is not stalled",
			active:       true,
			heartbeat:    now.UnixNano(),
			stallTimeout: time.Second,
			want:         false,
		},
		{
			name:         "active with old heartbeat past timeout is stalled",
			active:       true,
			heartbeat:    now.Add(-time.Minute).UnixNano(),
			stallTimeout: time.Second,
			want:         true,
		},
		{
			name:         "inactive flag overrides old heartbeat",
			active:       false,
			heartbeat:    now.Add(-time.Minute).UnixNano(),
			stallTimeout: time.Second,
			want:         false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			st := &State{ReaderActive: tt.active, Heartbeat: tt.heartbeat}
			if got := st.ReaderStalled(&mu, tt.stallTimeout); got != tt.want {
				t.Fatalf("ReaderStalled = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNextRestartBackoffLockedExponential(t *testing.T) {
	st := &State{}
	window := time.Minute
	maxRestarts := 5

	// First restart returns the initial delay; each subsequent doubles up to cap.
	want := []time.Duration{
		RestartBackoffInitial,     // 200ms
		RestartBackoffInitial * 2, // 400ms
		RestartBackoffInitial * 4, // 800ms
		RestartBackoffInitial * 8, // 1.6s
	}
	for i, w := range want {
		got, ok := st.NextRestartBackoffLocked(window, maxRestarts)
		if !ok {
			t.Fatalf("attempt %d: ok = false, want true (within budget)", i+1)
		}
		if got != w {
			t.Fatalf("attempt %d: backoff = %v, want %v", i+1, got, w)
		}
		if st.RestartCount != i+1 {
			t.Fatalf("attempt %d: RestartCount = %d, want %d", i+1, st.RestartCount, i+1)
		}
	}

	// Fifth attempt would double to 3.2s and is the last within budget (max 5).
	got, ok := st.NextRestartBackoffLocked(window, maxRestarts)
	if !ok {
		t.Fatalf("attempt 5: ok = false, want true (still within budget)")
	}
	if got != RestartBackoffInitial*16 {
		t.Fatalf("attempt 5: backoff = %v, want %v", got, RestartBackoffInitial*16)
	}
}

func TestNextRestartBackoffLockedCaps(t *testing.T) {
	st := &State{RestartBackoff: RestartBackoffCap}
	// Already at the cap: doubling must clamp back to the cap, not overflow it.
	got, ok := st.NextRestartBackoffLocked(time.Minute, 100)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got != RestartBackoffCap {
		t.Fatalf("backoff = %v, want capped at %v", got, RestartBackoffCap)
	}
	if st.RestartBackoff != RestartBackoffCap {
		t.Fatalf("RestartBackoff = %v, want %v", st.RestartBackoff, RestartBackoffCap)
	}
}

func TestNextRestartBackoffLockedExhaustsBudget(t *testing.T) {
	st := &State{}
	window := time.Minute
	maxRestarts := 3

	for i := 0; i < maxRestarts; i++ {
		if _, ok := st.NextRestartBackoffLocked(window, maxRestarts); !ok {
			t.Fatalf("attempt %d: ok = false, want true (within budget)", i+1)
		}
	}
	// The (maxRestarts+1)-th attempt exceeds the budget: give up and reset delay.
	got, ok := st.NextRestartBackoffLocked(window, maxRestarts)
	if ok {
		t.Fatal("ok = true after exhausting budget, want false")
	}
	if got != 0 {
		t.Fatalf("backoff = %v after exhausting budget, want 0", got)
	}
	if st.RestartBackoff != 0 {
		t.Fatalf("RestartBackoff = %v after exhausting budget, want 0", st.RestartBackoff)
	}
}

func TestNextRestartBackoffLockedZeroMaxRestarts(t *testing.T) {
	st := &State{}
	// A zero budget means the very first attempt already exceeds it.
	got, ok := st.NextRestartBackoffLocked(time.Minute, 0)
	if ok {
		t.Fatal("ok = true with maxRestarts=0, want false")
	}
	if got != 0 {
		t.Fatalf("backoff = %v, want 0", got)
	}
}

func TestNextRestartBackoffLockedWindowReset(t *testing.T) {
	window := time.Minute
	maxRestarts := 2

	// A window that opened longer ago than `window`: the count is reset so the
	// per-window restart budget refreshes and ok stays true even though the
	// previous count had exceeded the budget.
	st := &State{
		RestartSince: time.Now().Add(-2 * window), // last window opened long ago
		RestartCount: 99,
	}
	got, ok := st.NextRestartBackoffLocked(window, maxRestarts)
	if !ok {
		t.Fatal("ok = false after window reset, want true (budget refreshed)")
	}
	if got != RestartBackoffInitial {
		t.Fatalf("backoff = %v after window reset, want fresh %v", got, RestartBackoffInitial)
	}
	// The count is reset to zero by the rollover, then incremented for this call.
	if st.RestartCount != 1 {
		t.Fatalf("RestartCount = %d after window reset, want 1", st.RestartCount)
	}
	if st.RestartSince.IsZero() {
		t.Fatal("RestartSince is zero after window reset, want it set to now")
	}
}

func TestNextRestartBackoffLockedFirstCallSetsWindow(t *testing.T) {
	st := &State{} // zero RestartSince
	before := time.Now()
	if _, ok := st.NextRestartBackoffLocked(time.Minute, 5); !ok {
		t.Fatal("ok = false on first call, want true")
	}
	if st.RestartSince.Before(before) {
		t.Fatal("RestartSince was not set to ~now on first call")
	}
	if st.RestartCount != 1 {
		t.Fatalf("RestartCount = %d, want 1", st.RestartCount)
	}
}

func TestResetRestartBackoffLocked(t *testing.T) {
	st := &State{
		RestartBackoff: 4 * time.Second,
		RestartCount:   7,
		RestartSince:   time.Now(),
	}
	st.ResetRestartBackoffLocked()

	if st.RestartBackoff != 0 {
		t.Fatalf("RestartBackoff = %v, want 0", st.RestartBackoff)
	}
	if st.RestartCount != 0 {
		t.Fatalf("RestartCount = %d, want 0", st.RestartCount)
	}
	if !st.RestartSince.IsZero() {
		t.Fatalf("RestartSince = %v, want zero time", st.RestartSince)
	}
}

func TestResetRestartBackoffLockedOnZeroValueIsIdempotent(t *testing.T) {
	st := &State{}
	st.ResetRestartBackoffLocked()
	if st.RestartBackoff != 0 || st.RestartCount != 0 || !st.RestartSince.IsZero() {
		t.Fatalf("zero-value State changed after reset: %+v", st)
	}
}

// gatedReader blocks in Read until gate is closed, then returns io.EOF. It does
// not implement SetReadDeadline, so RunPTYReader's inner read goroutine blocks
// in r.Read exactly like a stalled PTY whose reader has not yet unwound.
type gatedReader struct {
	gate chan struct{}
}

func (g *gatedReader) Read([]byte) (int, error) {
	<-g.gate
	return 0, io.EOF
}

// TestStaleReaderExitDoesNotClobberReplacement drives the real
// stall -> restart -> stale-defer-fires interleave through actual reader and
// forwarder goroutines: a stalled reader is restarted, and when the stale
// reader finally unwinds, its deferred MarkReaderStopped must not clear the
// replacement reader's live bookkeeping.
//
// The restart is staged so the stale reader's cancel channel is closed inside
// StartReader while it holds the mutex. That orders the stale goroutine's
// deferred MarkReaderStopped strictly after the replacement's generation bump:
// closing the cancel outside the lock (as an explicit StopReader would) lets
// the stale cleanup race ahead of the bump and observe its own generation as
// current, which would let this test pass even against the pre-fix code.
func TestStaleReaderExitDoesNotClobberReplacement(t *testing.T) {
	var mu sync.Mutex
	st := &State{}

	// Reader #1: live but stalled, blocked in Read.
	gate1 := make(chan struct{})
	hA := newStartReaderHarness(&gatedReader{gate: gate1})
	st.StartReader(&mu, hA.opts)

	// The stall detector marks the reader inactive while its goroutine is still
	// alive; ReaderCancel is left in place for the restart to close.
	mu.Lock()
	st.ReaderActive = false
	mu.Unlock()

	// Reader #2: the replacement. StartReader closes reader #1's leftover cancel
	// under the lock, so reader #1 unwinds and its deferred MarkReaderStopped
	// runs against a State whose generation now belongs to reader #2.
	gate2 := make(chan struct{})
	hB := newStartReaderHarness(&gatedReader{gate: gate2})
	st.StartReader(&mu, hB.opts)

	// Reader #2's live bookkeeping must stay intact for the whole window during
	// which reader #1's stale cleanup can fire. Pre-fix the stale defer clears
	// ReaderActive/MsgCh; post-fix the generation guard makes it a no-op.
	testutil.Consistently(t, 200*time.Millisecond, time.Millisecond, func() string {
		mu.Lock()
		defer mu.Unlock()
		if !st.ReaderActive {
			return "stale reader #1 exit cleared reader #2's ReaderActive"
		}
		if st.MsgCh == nil {
			return "stale reader #1 exit cleared reader #2's MsgCh"
		}
		return ""
	})

	// Unwind both readers cleanly so no goroutine is left blocked in Read.
	close(gate1)
	hA.waitForward(t)
	close(gate2)
	hB.waitForward(t)
}
