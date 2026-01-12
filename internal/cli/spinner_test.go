package cli

import (
	"sync"
	"testing"
	"time"
)

func TestSpinner_StopMultipleTimes(t *testing.T) {
	// Test that calling Stop multiple times doesn't panic
	spinner := NewSpinner("test")
	spinner.Start()
	time.Sleep(10 * time.Millisecond) // Let spinner start

	// Should not panic on multiple stops
	spinner.Stop()
	spinner.Stop()
	spinner.Stop()
}

func TestSpinner_StopWithMessageMultipleTimes(t *testing.T) {
	// Test that calling StopWithMessage multiple times doesn't panic
	spinner := NewSpinner("test")
	spinner.Start()
	time.Sleep(10 * time.Millisecond)

	spinner.StopWithMessage("done")
	spinner.StopWithMessage("done again")
	spinner.Stop()
}

func TestSpinner_StopAndStopWithMessage(t *testing.T) {
	// Test mixing Stop and StopWithMessage
	spinner := NewSpinner("test")
	spinner.Start()
	time.Sleep(10 * time.Millisecond)

	spinner.Stop()
	spinner.StopWithMessage("message")
}

func TestSpinner_StopWithoutStart(t *testing.T) {
	// Test stopping a spinner that was never started
	spinner := NewSpinner("test")
	spinner.Stop() // Should not panic
	spinner.StopWithMessage("done")
}

func TestSpinner_ConcurrentStop(t *testing.T) {
	// Test concurrent stops don't cause race or panic
	spinner := NewSpinner("test")
	spinner.Start()
	time.Sleep(10 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			spinner.Stop()
		}()
	}
	wg.Wait()
}

func TestSpinner_UpdateMessage(t *testing.T) {
	spinner := NewSpinner("initial")
	spinner.Start()
	time.Sleep(10 * time.Millisecond)

	spinner.UpdateMessage("updated")
	time.Sleep(10 * time.Millisecond)

	spinner.Stop()
}

func TestWithSpinner_Success(t *testing.T) {
	err := WithSpinner("test operation", func() error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestWithSpinner_Error(t *testing.T) {
	expectedErr := &testError{msg: "test error"}
	err := WithSpinner("test operation", func() error {
		return expectedErr
	})
	if err != expectedErr {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
