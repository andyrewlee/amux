package supervisor

import (
	"context"
	"fmt"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"github.com/andyrewlee/amux/internal/logging"
)

// RestartPolicy controls when a worker should be restarted.
type RestartPolicy int

const (
	RestartNever RestartPolicy = iota
	RestartOnError
	RestartAlways
)

type options struct {
	policy      RestartPolicy
	maxRestarts int
	backoff     time.Duration
	maxBackoff  time.Duration
	onError     func(name string, err error)
	sleep       func(time.Duration)
	now         func() time.Time
}

// Option configures supervisor worker behavior.
type Option func(*options)

// WithRestartPolicy sets the restart policy.
func WithRestartPolicy(policy RestartPolicy) Option {
	return func(o *options) {
		o.policy = policy
	}
}

// WithMaxRestarts limits the number of restarts (0 = unlimited).
func WithMaxRestarts(maxRestarts int) Option {
	return func(o *options) {
		o.maxRestarts = maxRestarts
	}
}

// WithBackoff sets the initial backoff between restarts.
func WithBackoff(d time.Duration) Option {
	return func(o *options) {
		o.backoff = d
	}
}

// WithMaxBackoff caps the backoff between restarts.
func WithMaxBackoff(d time.Duration) Option {
	return func(o *options) {
		o.maxBackoff = d
	}
}

// WithSleep overrides the backoff sleep function (useful for deterministic tests).
func WithSleep(fn func(time.Duration)) Option {
	return func(o *options) {
		o.sleep = fn
	}
}

// withNow overrides the clock used to measure how long a worker ran (useful for
// deterministic tests of the backoff-reset logic).
func withNow(fn func() time.Time) Option {
	return func(o *options) {
		o.now = fn
	}
}

// StopTimeout bounds how long Stop waits for workers to exit. A worker that
// ignores context cancellation must not be able to wedge app shutdown.
const StopTimeout = 5 * time.Second

// Supervisor manages worker lifecycles with restart policies.
type Supervisor struct {
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	onError func(name string, err error)

	mu      sync.Mutex
	running map[string]int
}

// New creates a supervisor bound to the parent context.
func New(parent context.Context) *Supervisor {
	ctx, cancel := context.WithCancel(parent)
	return &Supervisor{ctx: ctx, cancel: cancel, running: make(map[string]int)}
}

// Context returns the supervisor context.
func (s *Supervisor) Context() context.Context {
	return s.ctx
}

// Stop cancels all workers and waits up to StopTimeout for them to exit,
// logging which workers failed to stop in time.
func (s *Supervisor) Stop() {
	if s == nil {
		return
	}
	s.StopWithTimeout(StopTimeout)
}

// StopWithTimeout cancels all workers and waits up to timeout for them to
// exit. On timeout it logs the workers still running and returns false.
func (s *Supervisor) StopWithTimeout(timeout time.Duration) bool {
	if s == nil {
		return true
	}
	s.cancel()
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		logging.Error("supervisor: timed out after %s waiting for workers to exit: %v", timeout, s.runningWorkers())
		return false
	}
}

// runningWorkers returns the names of workers that have not exited yet.
func (s *Supervisor) runningWorkers() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.running))
	for name, count := range s.running {
		if count > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func (s *Supervisor) markRunning(name string, delta int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running == nil {
		s.running = make(map[string]int)
	}
	s.running[name] += delta
	if s.running[name] <= 0 {
		delete(s.running, name)
	}
}

// SetErrorHandler registers a handler for worker errors.
func (s *Supervisor) SetErrorHandler(handler func(name string, err error)) {
	if s == nil {
		return
	}
	s.onError = handler
}

// Start runs a worker under supervision.
func (s *Supervisor) Start(name string, fn func(context.Context) error, opts ...Option) {
	if s == nil || fn == nil {
		return
	}
	cfg := options{
		policy:     RestartOnError,
		backoff:    200 * time.Millisecond,
		maxBackoff: 3 * time.Second,
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.onError == nil {
		cfg.onError = s.onError
	}
	if cfg.maxBackoff <= 0 {
		cfg.maxBackoff = cfg.backoff
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}

	s.wg.Add(1)
	s.markRunning(name, 1)
	go func() {
		defer s.wg.Done()
		defer s.markRunning(name, -1)
		restarts := 0
		backoff := cfg.backoff
		for {
			if s.ctx.Err() != nil {
				return
			}
			start := cfg.now()
			err := runSafe(s.ctx, name, fn)
			if s.ctx.Err() != nil {
				return
			}
			if err != nil && cfg.onError != nil {
				cfg.onError(name, err)
			}
			if !shouldRestart(err, cfg.policy) {
				return
			}
			// A worker that survived longer than maxBackoff looks recovered:
			// reset so the next failure restarts fast (and the restart counter)
			// instead of staying pinned at the capped backoff forever.
			if cfg.now().Sub(start) > cfg.maxBackoff {
				backoff = cfg.backoff
				restarts = 0
			}
			restarts++
			if cfg.maxRestarts > 0 && restarts > cfg.maxRestarts {
				logging.Error("supervisor: %s exceeded max restarts (%d)", name, cfg.maxRestarts)
				return
			}
			if backoff > 0 {
				if !sleepCtx(s.ctx, cfg, backoff) {
					return // context canceled during backoff
				}
				if backoff < cfg.maxBackoff {
					backoff *= 2
					if backoff > cfg.maxBackoff {
						backoff = cfg.maxBackoff
					}
				}
			}
		}
	}()
}

// sleepCtx waits for d or context cancellation. Returns false if canceled.
// cfg.sleep, when set by a test, replaces the timer entirely (tests stub it to
// a no-op, so cancellation during the wait is irrelevant there; we still report
// cancellation if the context is already done so the loop exits promptly).
func sleepCtx(ctx context.Context, cfg options, d time.Duration) bool {
	if cfg.sleep != nil {
		cfg.sleep(d)
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func shouldRestart(err error, policy RestartPolicy) bool {
	switch policy {
	case RestartAlways:
		return true
	case RestartOnError:
		return err != nil
	default:
		return false
	}
}

func runSafe(ctx context.Context, name string, fn func(context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in %s: %v", name, r)
			logging.Error("%v\n%s", err, debug.Stack())
		}
	}()
	return fn(ctx)
}
