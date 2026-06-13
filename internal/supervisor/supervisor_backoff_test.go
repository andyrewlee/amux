package supervisor

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/testutil"
)

func TestSupervisor_BackoffIsCancellable(t *testing.T) {
	ctx := context.Background()
	s := New(ctx)

	started := make(chan struct{}, 1)
	// Real timer (no WithSleep stub) with a large backoff. A worker mid-backoff
	// must observe context cancellation and return promptly instead of sleeping
	// out the full backoff.
	s.Start("flapping", func(ctx context.Context) error {
		select {
		case started <- struct{}{}:
		default:
		}
		return errors.New("fail")
	}, WithRestartPolicy(RestartOnError), WithBackoff(10*time.Second), WithMaxBackoff(10*time.Second))

	// Wait until the worker has failed at least once and is inside the backoff.
	<-started
	time.Sleep(50 * time.Millisecond)

	done := make(chan bool, 1)
	go func() {
		done <- s.StopWithTimeout(2 * time.Second)
	}()

	select {
	case clean := <-done:
		if !clean {
			t.Fatalf("StopWithTimeout returned false; backoff wait was not cancellable")
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("StopWithTimeout hung on an uninterruptible backoff sleep")
	}
}

func TestSupervisor_BackoffResetsAfterRecovery(t *testing.T) {
	ctx := context.Background()
	s := New(ctx)
	defer s.Stop()

	var (
		mu     sync.Mutex
		sleeps []time.Duration
		// virtual clock advanced by the worker body to simulate a long-running
		// (recovered) run between failures, deterministically.
		clock = time.Unix(0, 0)
	)
	now := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advance := func(d time.Duration) {
		mu.Lock()
		clock = clock.Add(d)
		mu.Unlock()
	}

	var callCount int32
	s.Start("recovering", func(ctx context.Context) error {
		count := atomic.AddInt32(&callCount, 1)
		switch count {
		case 1:
			// First failure: short-lived, should escalate the backoff.
			return errors.New("fail-1")
		case 2:
			// Survived a long time before failing again: this should reset
			// backoff to the base value rather than keeping the escalated one.
			advance(10 * time.Second)
			return errors.New("fail-2")
		default:
			return nil // stop the loop
		}
	},
		WithRestartPolicy(RestartOnError),
		WithBackoff(20*time.Millisecond),
		WithMaxBackoff(2*time.Second),
		withNow(now),
		WithSleep(func(d time.Duration) {
			mu.Lock()
			sleeps = append(sleeps, d)
			mu.Unlock()
		}),
	)

	testutil.WaitForAtomic(t, func() int32 { return atomic.LoadInt32(&callCount) }, 3, time.Second)

	mu.Lock()
	defer mu.Unlock()
	if len(sleeps) < 2 {
		t.Fatalf("expected at least 2 backoff samples, got %d: %v", len(sleeps), sleeps)
	}
	// After the first (short) failure the backoff escalates to 2x base = 40ms.
	if sleeps[0] != 20*time.Millisecond {
		t.Errorf("expected first backoff to be base 20ms, got %v", sleeps[0])
	}
	// After the second failure (which followed a long run) the backoff must
	// have reset to the base value instead of doubling further.
	if sleeps[1] != 20*time.Millisecond {
		t.Errorf("expected backoff to reset to base 20ms after recovery, got %v", sleeps[1])
	}
}

func TestSupervisor_BackoffRecoveryKeepsMaxRestartsCumulative(t *testing.T) {
	ctx := context.Background()
	s := New(ctx)
	defer s.Stop()

	var (
		mu     sync.Mutex
		clock  = time.Unix(0, 0)
		tooFar = make(chan struct{})
		once   sync.Once
	)
	now := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advance := func(d time.Duration) {
		mu.Lock()
		clock = clock.Add(d)
		mu.Unlock()
	}

	var callCount int32
	s.Start("recovering", func(ctx context.Context) error {
		count := atomic.AddInt32(&callCount, 1)
		if count > 3 {
			once.Do(func() { close(tooFar) })
		}
		advance(10 * time.Second)
		return errors.New("fail")
	},
		WithRestartPolicy(RestartOnError),
		WithMaxRestarts(2),
		WithBackoff(20*time.Millisecond),
		WithMaxBackoff(2*time.Second),
		withNow(now),
		WithSleep(func(time.Duration) {}),
	)

	testutil.WaitForAtomic(t, func() int32 { return atomic.LoadInt32(&callCount) }, 3, time.Second)

	select {
	case <-tooFar:
		t.Fatalf("expected max restarts to stop after 3 calls, got at least %d", atomic.LoadInt32(&callCount))
	case <-time.After(50 * time.Millisecond):
	}
}
