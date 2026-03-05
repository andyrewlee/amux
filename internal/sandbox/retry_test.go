package sandbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetry(t *testing.T) {
	t.Run("successful on first attempt", func(t *testing.T) {
		attempts := 0
		fn := func(ctx context.Context, attempt int) error {
			attempts++
			return nil
		}

		result := Retry(context.Background(), DefaultRetryConfig(), fn)
		if result.Error != nil {
			t.Errorf("Expected no error, got %v", result.Error)
		}
		if attempts != 1 {
			t.Errorf("Expected 1 attempt, got %d", attempts)
		}
		if result.Attempts != 1 {
			t.Errorf("Expected 1 recorded attempt, got %d", result.Attempts)
		}
	})

	t.Run("successful after retries", func(t *testing.T) {
		attempts := 0
		fn := func(ctx context.Context, attempt int) error {
			attempts++
			if attempts < 3 {
				return errors.New("temporary error")
			}
			return nil
		}

		cfg := RetryConfig{
			MaxAttempts:  5,
			InitialDelay: 1 * time.Millisecond,
			MaxDelay:     10 * time.Millisecond,
			Multiplier:   2.0,
		}

		result := Retry(context.Background(), cfg, fn)
		if result.Error != nil {
			t.Errorf("Expected no error, got %v", result.Error)
		}
		if attempts != 3 {
			t.Errorf("Expected 3 attempts, got %d", attempts)
		}
	})

	t.Run("failure after max attempts", func(t *testing.T) {
		attempts := 0
		fn := func(ctx context.Context, attempt int) error {
			attempts++
			return errors.New("persistent error")
		}

		cfg := RetryConfig{
			MaxAttempts:  3,
			InitialDelay: 1 * time.Millisecond,
			MaxDelay:     10 * time.Millisecond,
			Multiplier:   2.0,
		}

		result := Retry(context.Background(), cfg, fn)
		if result.Error == nil {
			t.Error("Expected error")
		}
		if attempts != 3 {
			t.Errorf("Expected 3 attempts, got %d", attempts)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		attempts := 0
		fn := func(ctx context.Context, attempt int) error {
			attempts++
			if attempts == 2 {
				cancel()
			}
			return errors.New("error")
		}

		cfg := RetryConfig{
			MaxAttempts:  10,
			InitialDelay: 1 * time.Millisecond,
			MaxDelay:     10 * time.Millisecond,
			Multiplier:   2.0,
		}

		result := Retry(ctx, cfg, fn)
		if result.Error == nil {
			t.Error("Expected error due to cancellation")
		}
		// Should stop after context is canceled
		if attempts > 3 {
			t.Errorf("Expected to stop soon after cancellation, got %d attempts", attempts)
		}
	})
}

func TestDefaultConfigs(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		cfg := DefaultRetryConfig()
		if cfg.MaxAttempts <= 0 {
			t.Error("Expected positive max attempts")
		}
		if cfg.InitialDelay <= 0 {
			t.Error("Expected positive initial delay")
		}
	})

	t.Run("SSH config", func(t *testing.T) {
		cfg := SSHRetryConfig()
		if cfg.MaxAttempts <= 0 {
			t.Error("Expected positive max attempts")
		}
		// SSH should have more retries than default
		if cfg.MaxAttempts < 3 {
			t.Error("Expected SSH config to have at least 3 retries")
		}
	})

	t.Run("network config", func(t *testing.T) {
		cfg := NetworkRetryConfig()
		if cfg.MaxAttempts <= 0 {
			t.Error("Expected positive max attempts")
		}
	})
}

func TestRetryResult(t *testing.T) {
	t.Run("duration tracking", func(t *testing.T) {
		fn := func(ctx context.Context, attempt int) error {
			time.Sleep(5 * time.Millisecond)
			return nil
		}

		cfg := RetryConfig{
			MaxAttempts:  1,
			InitialDelay: 1 * time.Millisecond,
			MaxDelay:     10 * time.Millisecond,
			Multiplier:   2.0,
		}

		result := Retry(context.Background(), cfg, fn)
		if result.Duration < 5*time.Millisecond {
			t.Errorf("Expected duration >= 5ms, got %v", result.Duration)
		}
	})
}

func TestCircuitBreaker(t *testing.T) {
	t.Run("basic circuit breaker", func(t *testing.T) {
		cb := NewCircuitBreaker(3, 100*time.Millisecond)

		// Should start closed
		if cb.IsOpen() {
			t.Error("Expected circuit to start closed")
		}

		// Record failures
		for i := 0; i < 3; i++ {
			_ = cb.Execute(func() error { return errors.New("fail") })
		}

		// Should be open now
		if !cb.IsOpen() {
			t.Error("Expected circuit to be open after failures")
		}

		// Should not allow execution when open
		err := cb.Execute(func() error { return nil })
		if err == nil {
			t.Error("Expected error when circuit is open")
		}
	})

	t.Run("circuit breaker recovery", func(t *testing.T) {
		cb := NewCircuitBreaker(2, 10*time.Millisecond)

		// Trigger circuit to open
		_ = cb.Execute(func() error { return errors.New("fail") })
		_ = cb.Execute(func() error { return errors.New("fail") })

		if !cb.IsOpen() {
			t.Error("Expected circuit to be open")
		}

		// Wait for reset timeout
		time.Sleep(15 * time.Millisecond)

		// Execute a successful request
		err := cb.Execute(func() error { return nil })
		if err != nil {
			t.Errorf("Expected no error after timeout, got %v", err)
		}

		// Should be closed now
		if cb.IsOpen() {
			t.Error("Expected circuit to be closed after success")
		}
	})

	t.Run("circuit breaker reset", func(t *testing.T) {
		cb := NewCircuitBreaker(2, 100*time.Millisecond)

		// Trigger circuit to open
		_ = cb.Execute(func() error { return errors.New("fail") })
		_ = cb.Execute(func() error { return errors.New("fail") })

		if !cb.IsOpen() {
			t.Error("Expected circuit to be open")
		}

		// Reset
		cb.Reset()

		if cb.IsOpen() {
			t.Error("Expected circuit to be closed after reset")
		}
	})

	t.Run("circuit breaker concurrent access", func(t *testing.T) {
		cb := NewCircuitBreaker(100, 100*time.Millisecond)

		// Run concurrent operations - this tests the mutex protection
		done := make(chan bool)
		for i := 0; i < 50; i++ {
			go func() {
				for j := 0; j < 20; j++ {
					_ = cb.Execute(func() error {
						if j%2 == 0 {
							return errors.New("fail")
						}
						return nil
					})
					_ = cb.IsOpen()
				}
				done <- true
			}()
		}

		// Wait for all goroutines
		for i := 0; i < 50; i++ {
			<-done
		}

		// Just verify it didn't panic or deadlock
	})

	t.Run("circuit breaker concurrent reset", func(t *testing.T) {
		cb := NewCircuitBreaker(5, 10*time.Millisecond)

		done := make(chan bool)

		// Concurrent failures
		for i := 0; i < 10; i++ {
			go func() {
				for j := 0; j < 10; j++ {
					_ = cb.Execute(func() error { return errors.New("fail") })
				}
				done <- true
			}()
		}

		// Concurrent resets
		for i := 0; i < 5; i++ {
			go func() {
				for j := 0; j < 5; j++ {
					cb.Reset()
					time.Sleep(time.Millisecond)
				}
				done <- true
			}()
		}

		// Wait for all
		for i := 0; i < 15; i++ {
			<-done
		}
	})
}
