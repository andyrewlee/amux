package app

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil"
)

func TestExternalMsgPumpConcurrent(t *testing.T) {
	app := &App{
		externalMsgs:     make(chan tea.Msg, 256),
		externalCritical: make(chan tea.Msg, 64),
	}

	var deliveredNormal, deliveredCritical int64
	app.SetMsgSender(func(msg tea.Msg) {
		switch msg.(type) {
		case messages.Error:
			atomic.AddInt64(&deliveredCritical, 1)
		default:
			atomic.AddInt64(&deliveredNormal, 1)
		}
	})

	// Producers count what the pump ACCEPTED (tryEnqueueExternalMsg's return).
	// The contract under flood: every accepted message is eventually
	// delivered while the channels are open — overflow drops are legal but
	// must never lose an accepted message or starve the critical queue.
	var acceptedNormal, acceptedCritical int64
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 2000; j++ {
				if app.tryEnqueueExternalMsg(testMsg("msg")) {
					atomic.AddInt64(&acceptedNormal, 1)
				}
			}
		}(i)
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				if app.tryEnqueueExternalMsg(messages.Error{Err: context.Canceled, Context: "race"}) {
					atomic.AddInt64(&acceptedCritical, 1)
				}
			}
		}()
	}

	wg.Wait()
	t.Cleanup(func() {
		close(app.externalMsgs)
		close(app.externalCritical)
	})

	// Critical producers offered 1000 against a 64-deep queue mid-flood —
	// drops are expected; what is NOT legal is dropping an accepted critical
	// or letting the normal flood starve them all.
	testutil.Eventually(t, 5*time.Second, 10*time.Millisecond, func() bool {
		return atomic.LoadInt64(&deliveredCritical) == atomic.LoadInt64(&acceptedCritical)
	}, "critical deliveries never converged: delivered=%d accepted=%d", atomic.LoadInt64(&deliveredCritical), atomic.LoadInt64(&acceptedCritical))
	if atomic.LoadInt64(&acceptedCritical) == 0 {
		t.Fatal("critical queue was starved entirely by the normal flood")
	}

	// With producers done the pump drains every accepted normal message —
	// assert completeness, not a vestigial >=1 floor.
	testutil.Eventually(t, 5*time.Second, 10*time.Millisecond, func() bool {
		return atomic.LoadInt64(&deliveredNormal) == atomic.LoadInt64(&acceptedNormal)
	}, "normal deliveries never converged: delivered=%d accepted=%d", atomic.LoadInt64(&deliveredNormal), atomic.LoadInt64(&acceptedNormal))
}
