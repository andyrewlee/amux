package sandbox

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

// RetryConfig configures retry behavior.
type RetryConfig struct {
	MaxAttempts     int           // Maximum number of attempts (0 = infinite)
	InitialDelay    time.Duration // Initial delay between retries
	MaxDelay        time.Duration // Maximum delay between retries
	Multiplier      float64       // Multiplier for exponential backoff
	Jitter          float64       // Jitter factor (0-1) to add randomness
	RetryableErrors []error       // Specific errors that should be retried (nil = retry all)
}

// DefaultRetryConfig returns sensible defaults for retry behavior.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  5,
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		Jitter:       0.1,
	}
}

// SSHRetryConfig returns retry config optimized for SSH connections.
func SSHRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  10,
		InitialDelay: 500 * time.Millisecond,
		MaxDelay:     15 * time.Second,
		Multiplier:   1.5,
		Jitter:       0.2,
	}
}

// NetworkRetryConfig returns retry config for network operations.
func NetworkRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 2 * time.Second,
		MaxDelay:     10 * time.Second,
		Multiplier:   2.0,
		Jitter:       0.1,
	}
}

// RetryResult contains the result of a retry operation.
type RetryResult struct {
	Attempts int
	Duration time.Duration
	Error    error
}

// RetryFunc is a function that can be retried.
type RetryFunc func(ctx context.Context, attempt int) error

// Retry executes a function with exponential backoff retry.
func Retry(ctx context.Context, cfg RetryConfig, fn RetryFunc) RetryResult {
	start := time.Now()
	result := RetryResult{}

	for attempt := 1; cfg.MaxAttempts == 0 || attempt <= cfg.MaxAttempts; attempt++ {
		result.Attempts = attempt

		// Check context before attempting
		if ctx.Err() != nil {
			result.Error = ctx.Err()
			result.Duration = time.Since(start)
			return result
		}

		// Execute the function
		err := fn(ctx, attempt)
		if err == nil {
			result.Duration = time.Since(start)
			return result
		}

		// Check if error is retryable
		if !isRetryableError(err, cfg.RetryableErrors) {
			result.Error = err
			result.Duration = time.Since(start)
			return result
		}

		// Check if we've exhausted attempts
		if cfg.MaxAttempts > 0 && attempt >= cfg.MaxAttempts {
			result.Error = fmt.Errorf("max retries (%d) exceeded: %w", cfg.MaxAttempts, err)
			result.Duration = time.Since(start)
			return result
		}

		// Calculate delay with exponential backoff and jitter
		delay := calculateDelay(cfg, attempt)

		// Log retry attempt
		LogDebug("retrying operation",
			"attempt", attempt,
			"maxAttempts", cfg.MaxAttempts,
			"delay", delay,
			"error", err,
		)

		// Wait before next attempt
		select {
		case <-ctx.Done():
			result.Error = ctx.Err()
			result.Duration = time.Since(start)
			return result
		case <-time.After(delay):
			// Continue to next attempt
		}
	}

	result.Duration = time.Since(start)
	return result
}

// RetryWithResult is like Retry but returns a value.
func RetryWithResult[T any](ctx context.Context, cfg RetryConfig, fn func(ctx context.Context, attempt int) (T, error)) (T, RetryResult) {
	var result T
	start := time.Now()
	retryResult := RetryResult{}

	for attempt := 1; cfg.MaxAttempts == 0 || attempt <= cfg.MaxAttempts; attempt++ {
		retryResult.Attempts = attempt

		if ctx.Err() != nil {
			retryResult.Error = ctx.Err()
			retryResult.Duration = time.Since(start)
			return result, retryResult
		}

		var err error
		result, err = fn(ctx, attempt)
		if err == nil {
			retryResult.Duration = time.Since(start)
			return result, retryResult
		}

		if !isRetryableError(err, cfg.RetryableErrors) {
			retryResult.Error = err
			retryResult.Duration = time.Since(start)
			return result, retryResult
		}

		if cfg.MaxAttempts > 0 && attempt >= cfg.MaxAttempts {
			retryResult.Error = fmt.Errorf("max retries (%d) exceeded: %w", cfg.MaxAttempts, err)
			retryResult.Duration = time.Since(start)
			return result, retryResult
		}

		delay := calculateDelay(cfg, attempt)

		select {
		case <-ctx.Done():
			retryResult.Error = ctx.Err()
			retryResult.Duration = time.Since(start)
			return result, retryResult
		case <-time.After(delay):
		}
	}

	retryResult.Duration = time.Since(start)
	return result, retryResult
}

func calculateDelay(cfg RetryConfig, attempt int) time.Duration {
	// Exponential backoff: initialDelay * multiplier^(attempt-1)
	delay := float64(cfg.InitialDelay) * math.Pow(cfg.Multiplier, float64(attempt-1))

	// Apply jitter
	if cfg.Jitter > 0 {
		jitterRange := delay * cfg.Jitter
		delay += (rand.Float64()*2 - 1) * jitterRange
	}

	// Cap at max delay
	if delay > float64(cfg.MaxDelay) {
		delay = float64(cfg.MaxDelay)
	}

	// Ensure non-negative
	if delay < 0 {
		delay = float64(cfg.InitialDelay)
	}

	return time.Duration(delay)
}

func isRetryableError(err error, retryableErrors []error) bool {
	// If no specific errors defined, check if it's a SandboxError with Retryable flag
	if len(retryableErrors) == 0 {
		if se := GetSandboxError(err); se != nil {
			return se.Retryable
		}
		// Default: retry all errors
		return true
	}

	// Check against specific retryable errors
	for _, retryable := range retryableErrors {
		if errors.Is(err, retryable) {
			return true
		}
	}
	return false
}

// CircuitBreaker prevents repeated failures from overwhelming a service.
// It is safe for concurrent use.
type CircuitBreaker struct {
	mu            sync.Mutex
	maxFailures   int
	resetTimeout  time.Duration
	failures      int
	lastFailure   time.Time
	state         circuitState
	halfOpenLimit int
	halfOpenCount int
}

type circuitState int

const (
	circuitClosed circuitState = iota
	circuitOpen
	circuitHalfOpen
)

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures:   maxFailures,
		resetTimeout:  resetTimeout,
		state:         circuitClosed,
		halfOpenLimit: 1,
	}
}

// Execute runs a function through the circuit breaker.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.canExecute() {
		return errors.New("circuit breaker is open")
	}

	err := fn()
	cb.recordResult(err)
	return err
}

func (cb *CircuitBreaker) canExecute() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case circuitClosed:
		return true
	case circuitOpen:
		if time.Since(cb.lastFailure) > cb.resetTimeout {
			cb.state = circuitHalfOpen
			cb.halfOpenCount = 0
			return true
		}
		return false
	case circuitHalfOpen:
		return cb.halfOpenCount < cb.halfOpenLimit
	}
	return false
}

func (cb *CircuitBreaker) recordResult(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err == nil {
		cb.onSuccessLocked()
	} else {
		cb.onFailureLocked()
	}
}

// onSuccessLocked must be called with cb.mu held.
func (cb *CircuitBreaker) onSuccessLocked() {
	cb.failures = 0
	if cb.state == circuitHalfOpen {
		cb.state = circuitClosed
	}
}

// onFailureLocked must be called with cb.mu held.
func (cb *CircuitBreaker) onFailureLocked() {
	cb.failures++
	cb.lastFailure = time.Now()

	if cb.state == circuitHalfOpen {
		cb.state = circuitOpen
		return
	}

	if cb.failures >= cb.maxFailures {
		cb.state = circuitOpen
	}
}

// IsOpen returns true if the circuit breaker is open.
func (cb *CircuitBreaker) IsOpen() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state == circuitOpen
}

// Reset resets the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = circuitClosed
	cb.failures = 0
	cb.halfOpenCount = 0
}
