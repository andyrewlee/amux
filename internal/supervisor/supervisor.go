package supervisor

import (
	"context"
	"fmt"
	"runtime/debug"
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
func WithMaxRestarts(max int) Option {
	return func(o *options) {
		o.maxRestarts = max
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

// Supervisor manages worker lifecycles with restart policies.
type Supervisor struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	onError func(name string, err error)
}

// New creates a supervisor bound to the parent context.
func New(parent context.Context) *Supervisor {
	ctx, cancel := context.WithCancel(parent)
	return &Supervisor{ctx: ctx, cancel: cancel}
}

// Context returns the supervisor context.
func (s *Supervisor) Context() context.Context {
	return s.ctx
}

// Stop cancels all workers and waits for them to exit.
func (s *Supervisor) Stop() {
	if s == nil {
		return
	}
	s.cancel()
	s.wg.Wait()
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

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		restarts := 0
		backoff := cfg.backoff
		for {
			if s.ctx.Err() != nil {
				return
			}
			err := runSafe(name, fn, s.ctx)
			if s.ctx.Err() != nil {
				return
			}
			if err != nil && cfg.onError != nil {
				cfg.onError(name, err)
			}
			if !shouldRestart(err, cfg.policy) {
				return
			}
			restarts++
			if cfg.maxRestarts > 0 && restarts > cfg.maxRestarts {
				logging.Error("supervisor: %s exceeded max restarts (%d)", name, cfg.maxRestarts)
				return
			}
			if backoff > 0 {
				time.Sleep(backoff)
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

func runSafe(name string, fn func(context.Context) error, ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in %s: %v", name, r)
			logging.Error("%v\n%s", err, debug.Stack())
		}
	}()
	return fn(ctx)
}
