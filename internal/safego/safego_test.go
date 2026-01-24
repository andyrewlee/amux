package safego

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRun_NoPanic(t *testing.T) {
	var called bool
	Run("test", func() {
		called = true
	})
	if !called {
		t.Error("function was not called")
	}
}

func TestRun_RecoversPanic(t *testing.T) {
	// Should not panic the test
	Run("test-panic", func() {
		panic("test panic")
	})
}

func TestRun_CallsPanicHandler(t *testing.T) {
	var (
		mu            sync.Mutex
		handlerCalled bool
		handlerName   string
		handlerValue  any
	)

	SetPanicHandler(func(name string, recovered any, stack []byte) {
		mu.Lock()
		handlerCalled = true
		handlerName = name
		handlerValue = recovered
		mu.Unlock()
	})
	defer SetPanicHandler(nil)

	Run("my-goroutine", func() {
		panic("oops")
	})

	mu.Lock()
	defer mu.Unlock()

	if !handlerCalled {
		t.Error("panic handler was not called")
	}
	if handlerName != "my-goroutine" {
		t.Errorf("expected name 'my-goroutine', got %q", handlerName)
	}
	if handlerValue != "oops" {
		t.Errorf("expected recovered value 'oops', got %v", handlerValue)
	}
}

func TestRun_PanicHandlerPanicIsRecovered(t *testing.T) {
	SetPanicHandler(func(name string, recovered any, stack []byte) {
		panic("handler panic")
	})
	defer SetPanicHandler(nil)

	// Should not panic the test even if the handler panics
	Run("test", func() {
		panic("original panic")
	})
}

func TestRun_EmptyName(t *testing.T) {
	var (
		mu          sync.Mutex
		handlerName string
	)

	SetPanicHandler(func(name string, recovered any, stack []byte) {
		mu.Lock()
		handlerName = name
		mu.Unlock()
	})
	defer SetPanicHandler(nil)

	Run("", func() {
		panic("test")
	})

	mu.Lock()
	defer mu.Unlock()

	if handlerName != "goroutine" {
		t.Errorf("expected default name 'goroutine', got %q", handlerName)
	}
}

func TestGo_RunsInGoroutine(t *testing.T) {
	var wg sync.WaitGroup
	var called int32

	wg.Add(1)
	Go("test", func() {
		atomic.StoreInt32(&called, 1)
		wg.Done()
	})

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if atomic.LoadInt32(&called) != 1 {
			t.Error("function was not called")
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for goroutine")
	}
}

func TestGo_RecoversPanic(t *testing.T) {
	var wg sync.WaitGroup
	var handlerCalled int32

	SetPanicHandler(func(name string, recovered any, stack []byte) {
		atomic.StoreInt32(&handlerCalled, 1)
		wg.Done()
	})
	defer SetPanicHandler(nil)

	wg.Add(1)
	Go("test-panic", func() {
		panic("goroutine panic")
	})

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if atomic.LoadInt32(&handlerCalled) != 1 {
			t.Error("panic handler was not called")
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for panic handler")
	}
}

func TestSetPanicHandler_Concurrent(t *testing.T) {
	// Test that concurrent access to the panic handler is safe
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			SetPanicHandler(func(name string, recovered any, stack []byte) {})
		}()
		go func() {
			defer wg.Done()
			Run("test", func() {
				panic("test")
			})
		}()
	}
	wg.Wait()
	SetPanicHandler(nil)
}
