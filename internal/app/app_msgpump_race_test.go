package app

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
)

func TestExternalMsgPumpConcurrent(t *testing.T) {
	app := &App{
		externalMsgs:     make(chan tea.Msg, 256),
		externalCritical: make(chan tea.Msg, 64),
	}

	var delivered int64
	app.SetMsgSender(func(msg tea.Msg) {
		_ = msg
		atomic.AddInt64(&delivered, 1)
	})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 2000; j++ {
				app.enqueueExternalMsg(testMsg("msg"))
			}
		}(i)
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				app.enqueueExternalMsg(messages.Error{Err: context.Canceled, Context: "race"})
			}
		}()
	}

	wg.Wait()
	close(app.externalMsgs)
	close(app.externalCritical)

	if !waitForCount(&delivered, 1, 2*time.Second) {
		t.Fatalf("expected some messages delivered")
	}
}

func waitForCount(val *int64, want int64, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(val) >= want {
			return true
		}
		time.Sleep(1 * time.Millisecond)
	}
	return atomic.LoadInt64(val) >= want
}
