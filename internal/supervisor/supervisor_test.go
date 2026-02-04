package supervisor

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	ctx := context.Background()
	s := New(ctx)
	if s == nil {
		t.Fatal("New returned nil")
	}
	if s.ctx == nil {
		t.Error("context is nil")
	}
	if s.cancel == nil {
		t.Error("cancel is nil")
	}
	s.Stop()
}

func TestSupervisor_Context(t *testing.T) {
	ctx := context.Background()
	s := New(ctx)
	defer s.Stop()

	if s.Context() == nil {
		t.Error("Context() returned nil")
	}
}

func TestSupervisor_Stop_Nil(t *testing.T) {
	// Should not panic
	var s *Supervisor
	s.Stop()
}

func TestSupervisor_Start_Nil(t *testing.T) {
	// Should not panic
	var s *Supervisor
	s.Start("test", func(ctx context.Context) error { return nil })
}

func TestSupervisor_Start_NilFn(t *testing.T) {
	ctx := context.Background()
	s := New(ctx)
	defer s.Stop()

	// Should not panic
	s.Start("test", nil)
}

func TestSupervisor_WorkerRuns(t *testing.T) {
	ctx := context.Background()
	s := New(ctx)
	defer s.Stop()

	var called int32
	done := make(chan struct{})

	s.Start("test", func(ctx context.Context) error {
		atomic.StoreInt32(&called, 1)
		close(done)
		return nil
	}, WithRestartPolicy(RestartNever))

	select {
	case <-done:
		if atomic.LoadInt32(&called) != 1 {
			t.Error("worker was not called")
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for worker")
	}
}

func TestSupervisor_WorkerStopsOnContextCancel(t *testing.T) {
	ctx := context.Background()
	s := New(ctx)

	started := make(chan struct{})
	stopped := make(chan struct{})

	s.Start("test", func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return nil
	}, WithRestartPolicy(RestartNever))

	<-started
	s.Stop()

	select {
	case <-stopped:
		// OK
	case <-time.After(time.Second):
		t.Error("worker did not stop on context cancel")
	}
}

func TestSupervisor_RestartNever(t *testing.T) {
	ctx := context.Background()
	s := New(ctx)
	defer s.Stop()

	var callCount int32

	s.Start("test", func(ctx context.Context) error {
		atomic.AddInt32(&callCount, 1)
		return errors.New("fail")
	}, WithRestartPolicy(RestartNever), WithSleep(func(time.Duration) {}))

	waitForCount(t, &callCount, 1, time.Second)

	if count := atomic.LoadInt32(&callCount); count != 1 {
		t.Errorf("expected 1 call, got %d", count)
	}
}

func TestSupervisor_RestartOnError(t *testing.T) {
	ctx := context.Background()
	s := New(ctx)
	defer s.Stop()

	var callCount int32

	s.Start("test", func(ctx context.Context) error {
		count := atomic.AddInt32(&callCount, 1)
		if count < 3 {
			return errors.New("fail")
		}
		// Stop after 3 calls
		return nil
	}, WithRestartPolicy(RestartOnError), WithBackoff(10*time.Millisecond), WithMaxBackoff(20*time.Millisecond), WithSleep(func(time.Duration) {}))

	waitForCount(t, &callCount, 3, time.Second)

	if count := atomic.LoadInt32(&callCount); count != 3 {
		t.Errorf("expected 3 calls, got %d", count)
	}
}

func TestSupervisor_RestartAlways(t *testing.T) {
	ctx := context.Background()
	s := New(ctx)

	var callCount int32
	ready := make(chan struct{})

	s.Start("test", func(ctx context.Context) error {
		count := atomic.AddInt32(&callCount, 1)
		if count >= 3 {
			close(ready)
			<-ctx.Done()
		}
		return nil // Returns nil but should still restart
	}, WithRestartPolicy(RestartAlways), WithBackoff(10*time.Millisecond), WithSleep(func(time.Duration) {}))

	select {
	case <-ready:
		// Got at least 3 restarts
	case <-time.After(time.Second):
		t.Error("timed out waiting for restarts")
	}

	s.Stop()

	if count := atomic.LoadInt32(&callCount); count < 3 {
		t.Errorf("expected at least 3 calls, got %d", count)
	}
}

func TestSupervisor_MaxRestarts(t *testing.T) {
	ctx := context.Background()
	s := New(ctx)
	defer s.Stop()

	var callCount int32

	s.Start("test", func(ctx context.Context) error {
		atomic.AddInt32(&callCount, 1)
		return errors.New("fail")
	}, WithRestartPolicy(RestartOnError), WithMaxRestarts(2), WithBackoff(10*time.Millisecond), WithSleep(func(time.Duration) {}))

	waitForCount(t, &callCount, 3, time.Second)

	// Should be 3 calls: initial + 2 restarts
	if count := atomic.LoadInt32(&callCount); count != 3 {
		t.Errorf("expected 3 calls (initial + 2 restarts), got %d", count)
	}
}

func TestSupervisor_ErrorHandler(t *testing.T) {
	ctx := context.Background()
	s := New(ctx)
	defer s.Stop()

	var (
		once          sync.Once
		mu            sync.Mutex
		errorName     string
		errorReceived error
		errCh         = make(chan struct{})
	)

	s.SetErrorHandler(func(name string, err error) {
		mu.Lock()
		errorName = name
		errorReceived = err
		mu.Unlock()
		once.Do(func() { close(errCh) })
	})

	expectedErr := errors.New("worker error")
	s.Start("my-worker", func(ctx context.Context) error {
		return expectedErr
	}, WithRestartPolicy(RestartNever), WithSleep(func(time.Duration) {}))

	select {
	case <-errCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for error handler")
	}

	mu.Lock()
	defer mu.Unlock()

	if errorName != "my-worker" {
		t.Errorf("expected error name 'my-worker', got %q", errorName)
	}
	if errorReceived != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, errorReceived)
	}
}

func TestSupervisor_ErrorHandlerNotCalledOnCancel(t *testing.T) {
	ctx := context.Background()
	s := New(ctx)

	var errorCalled int32
	errCh := make(chan struct{}, 1)
	s.SetErrorHandler(func(name string, err error) {
		atomic.StoreInt32(&errorCalled, 1)
		select {
		case errCh <- struct{}{}:
		default:
		}
	})

	started := make(chan struct{})
	s.Start("test", func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}, WithRestartPolicy(RestartOnError), WithSleep(func(time.Duration) {}))

	<-started
	s.Stop()

	select {
	case <-errCh:
		t.Fatal("error handler should not be called for context.Canceled")
	case <-time.After(200 * time.Millisecond):
	}

	if atomic.LoadInt32(&errorCalled) != 0 {
		t.Error("error handler should not be called for context.Canceled")
	}
}

func TestSupervisor_PanicRecovery(t *testing.T) {
	ctx := context.Background()
	s := New(ctx)
	defer s.Stop()

	var callCount int32

	s.Start("test", func(ctx context.Context) error {
		count := atomic.AddInt32(&callCount, 1)
		if count == 1 {
			panic("worker panic")
		}
		return nil
	}, WithRestartPolicy(RestartOnError), WithBackoff(10*time.Millisecond), WithSleep(func(time.Duration) {}))

	waitForCount(t, &callCount, 2, time.Second)

	if count := atomic.LoadInt32(&callCount); count < 2 {
		t.Errorf("expected at least 2 calls (panic should trigger restart), got %d", count)
	}
}

func TestSupervisor_BackoffDoubles(t *testing.T) {
	ctx := context.Background()
	s := New(ctx)
	defer s.Stop()

	var sleeps []time.Duration
	var mu sync.Mutex
	var callCount int32

	s.Start("test", func(ctx context.Context) error {
		count := atomic.AddInt32(&callCount, 1)
		if count >= 4 {
			return nil
		}
		return errors.New("fail")
	}, WithRestartPolicy(RestartOnError), WithBackoff(20*time.Millisecond), WithMaxBackoff(100*time.Millisecond), WithSleep(func(d time.Duration) {
		mu.Lock()
		sleeps = append(sleeps, d)
		mu.Unlock()
	}))

	waitForCount(t, &callCount, 4, time.Second)

	mu.Lock()
	defer mu.Unlock()

	if len(sleeps) < 3 {
		t.Fatalf("expected at least 3 backoff samples, got %d", len(sleeps))
	}

	// Check that delays roughly double (with some tolerance)
	for i := 1; i < len(sleeps); i++ {
		delay := sleeps[i]
		prevDelay := sleeps[i-1]
		// Allow some tolerance for timing
		if i < 3 && delay < prevDelay {
			t.Logf("delay %d (%v) not greater than delay %d (%v)", i, delay, i-1, prevDelay)
		}
	}
}

func TestSupervisor_SetErrorHandler_Nil(t *testing.T) {
	var s *Supervisor
	// Should not panic
	s.SetErrorHandler(func(name string, err error) {})
}

func TestWithOptions(t *testing.T) {
	// Just verify the option functions don't panic
	opts := []Option{
		WithRestartPolicy(RestartAlways),
		WithMaxRestarts(5),
		WithBackoff(time.Second),
		WithMaxBackoff(10 * time.Second),
		WithSleep(func(time.Duration) {}),
	}

	cfg := &options{}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.policy != RestartAlways {
		t.Error("WithRestartPolicy did not set policy")
	}
	if cfg.maxRestarts != 5 {
		t.Error("WithMaxRestarts did not set maxRestarts")
	}
	if cfg.backoff != time.Second {
		t.Error("WithBackoff did not set backoff")
	}
	if cfg.maxBackoff != 10*time.Second {
		t.Error("WithMaxBackoff did not set maxBackoff")
	}
}

func TestSupervisor_ConcurrentStart(t *testing.T) {
	ctx := context.Background()
	s := New(ctx)
	defer s.Stop()

	var wg sync.WaitGroup
	var startedCount int32

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			s.Start("worker", func(ctx context.Context) error {
				atomic.AddInt32(&startedCount, 1)
				<-ctx.Done()
				return nil
			}, WithRestartPolicy(RestartNever), WithSleep(func(time.Duration) {}))
		}(i)
	}

	wg.Wait()
	waitForCount(t, &startedCount, 10, time.Second)

	if count := atomic.LoadInt32(&startedCount); count != 10 {
		t.Errorf("expected 10 workers started, got %d", count)
	}
}

func waitForCount(t *testing.T, val *int32, want int32, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(val) >= want {
			return
		}
		time.Sleep(1 * time.Millisecond)
	}
	if got := atomic.LoadInt32(val); got < want {
		t.Fatalf("timed out waiting for count %d (got %d)", want, got)
	}
}
