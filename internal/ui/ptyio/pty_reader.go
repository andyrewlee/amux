package ptyio

import (
	"errors"
	"io"
	"os"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/safego"
)

const (
	ptyIdleHeartbeatInterval = time.Second
	ptyReadDeadlineInterval  = 250 * time.Millisecond
)

type readDeadliner interface {
	SetReadDeadline(time.Time) error
}

// PTYReaderConfig configures the shared PTY read loop.
type PTYReaderConfig struct {
	Label           string // safego goroutine label
	ReadBufferSize  int
	ReadQueueSize   int
	FrameInterval   time.Duration
	MaxPendingBytes int
}

// PTYMsgFactory creates tea.Msg values from PTY events.
// Closures capture the WorkspaceID/TabID from the call site.
type PTYMsgFactory struct {
	Output  func(data []byte) tea.Msg
	Stopped func(err error) tea.Msg
}

// RunPTYReader reads from r, buffers bytes, sends Output messages via msgCh
// on ticker ticks or when MaxPendingBytes is hit. Sends Stopped on error.
// msgCh is closed exactly once, by this goroutine, on every return path
// (including panic) via the deferred close below, so ForwardPTYMsgs never
// blocks on a channel that will not close.
func RunPTYReader(
	r io.Reader, msgCh chan tea.Msg, cancel <-chan struct{},
	heartbeat *int64, cfg PTYReaderConfig, factory PTYMsgFactory,
) {
	// This goroutine is the sole owner of msgCh, so close it once on return.
	// A deferred close runs during panic unwinding too, which unblocks
	// ForwardPTYMsgs before the panic propagates to safego.Run (which logs it).
	defer close(msgCh)

	if r == nil {
		return
	}
	beat := func() {
		if heartbeat != nil {
			atomic.StoreInt64(heartbeat, time.Now().UnixNano())
		}
	}
	beat()

	// readEvent is one Read outcome published on the single ordered stream:
	// the bytes the call returned plus its error. A real Read may legally
	// return both, so terminal events can carry data — appending first, then
	// handling the error, is what keeps the stream's byte order intact. The
	// producer emits at most one terminal event, always last.
	type readEvent struct {
		data     []byte
		err      error
		terminal bool
	}
	events := make(chan readEvent, cfg.ReadQueueSize)

	safego.Go(cfg.Label, func() {
		deadliner, deadlineSupported := r.(readDeadliner)
		defer func() {
			if deadlineSupported {
				_ = deadliner.SetReadDeadline(time.Time{})
			}
			close(events)
		}()
		publish := func(ev readEvent) bool {
			select {
			case events <- ev:
				return true
			case <-cancel:
				return false
			}
		}
		buf := make([]byte, cfg.ReadBufferSize)
		for {
			select {
			case <-cancel:
				return
			default:
			}
			if deadlineSupported {
				if err := deadliner.SetReadDeadline(time.Now().Add(ptyReadDeadlineInterval)); err != nil {
					deadlineSupported = false
				}
			}
			n, err := r.Read(buf)
			var chunk []byte
			if n > 0 {
				beat()
				chunk = make([]byte, n)
				copy(chunk, buf[:n])
			}
			switch {
			case isReadTimeout(err):
				// A deadline poll: a retry signal, not a terminator. Any
				// bytes the same call returned are still real stream data.
				if chunk != nil && !publish(readEvent{data: chunk}) {
					return
				}
				continue
			case err != nil:
				// Terminal: the bytes and the error are one event so queued
				// output can never overtake or be overtaken by termination.
				_ = publish(readEvent{data: chunk, err: err, terminal: true})
				return
			case chunk != nil:
				if !publish(readEvent{data: chunk}) {
					return
				}
			}
		}
	})

	heartbeatTicker := time.NewTicker(ptyIdleHeartbeatInterval)
	defer heartbeatTicker.Stop()
	var flushTicker *time.Ticker
	var flushTick <-chan time.Time
	startFlushTicker := func() {
		if flushTicker != nil {
			return
		}
		flushInterval := cfg.FrameInterval
		if flushInterval <= 0 {
			flushInterval = 40 * time.Millisecond
		}
		flushTicker = time.NewTicker(flushInterval)
		flushTick = flushTicker.C
	}
	stopFlushTicker := func() {
		if flushTicker == nil {
			return
		}
		flushTicker.Stop()
		flushTicker = nil
		flushTick = nil
	}
	defer stopFlushTicker()

	var pending []byte
	// flushPending sends the coalesced remainder and returns whether the send
	// completed; shared by the terminal paths and the size/tick flushes.
	flushPending := func() bool {
		if len(pending) == 0 {
			return true
		}
		if !SendPTYMsg(msgCh, cancel, factory.Output(pending)) {
			return false
		}
		pending = nil
		return true
	}

	for {
		select {
		case <-cancel:
			return
		case ev, ok := <-events:
			beat()
			if !ok {
				// Producer closed without a terminal event (panic unwind or a
				// cancel-side return): deliver what is still pending, then
				// report clean EOF as before.
				if !flushPending() {
					return
				}
				SendPTYMsg(msgCh, cancel, factory.Stopped(io.EOF))
				return
			}
			// Append the event's bytes before handling its error: a terminal
			// event can legally carry the stream's final chunk.
			if len(ev.data) > 0 {
				// Adopt the read chunk when the coalescing buffer is empty:
				// the read goroutine drops its reference after sending, so
				// ownership transfers here and the append-copy is only
				// needed when merging.
				if len(pending) == 0 {
					pending = ev.data
				} else {
					pending = append(pending, ev.data...)
				}
				startFlushTicker()
			}
			if ev.terminal {
				// Every accepted byte is now in pending — deliver it, then
				// the one Stopped. Nothing behind this event can arrive.
				if !flushPending() {
					return
				}
				SendPTYMsg(msgCh, cancel, factory.Stopped(ev.err))
				return
			}
			if len(pending) >= cfg.MaxPendingBytes {
				if !flushPending() {
					return
				}
				stopFlushTicker()
			}
		case <-flushTick:
			beat()
			if !flushPending() {
				return
			}
			stopFlushTicker()
		case <-heartbeatTicker.C:
			beat()
		}
	}
}

func isReadTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}

// SendPTYMsg sends msg on msgCh, returning false if cancel fires first.
func SendPTYMsg(msgCh chan tea.Msg, cancel <-chan struct{}, msg tea.Msg) bool {
	if msgCh == nil {
		return false
	}
	select {
	case <-cancel:
		return false
	case msgCh <- msg:
		return true
	}
}

// OutputMerger configures how ForwardPTYMsgs merges consecutive output messages.
type OutputMerger struct {
	ExtractData func(msg tea.Msg) ([]byte, bool)         // type-assert + return Data
	CanMerge    func(current, next tea.Msg) bool         // same workspace+tab?
	Build       func(first tea.Msg, data []byte) tea.Msg // clone with merged data
	MaxPending  int
}

// ForwardPTYMsgs reads from msgCh, merges consecutive output messages, forwards via sink.
func ForwardPTYMsgs(msgCh <-chan tea.Msg, sink func(tea.Msg), merger OutputMerger) {
	for msg := range msgCh {
		if msg == nil {
			continue
		}
		data, ok := merger.ExtractData(msg)
		if !ok {
			if sink != nil {
				sink(msg)
			}
			continue
		}

		// Adopt the first message's data as the merge buffer: the upstream
		// reader drops its reference when the message is sent, and Build
		// replaces .Data with the merged slice, so this goroutine holds the
		// only reference. Appends may grow into the slice's spare capacity;
		// nothing reads the consumed message's data past its original length.
		merged := data
		first := msg
		for {
			select {
			case next, ok := <-msgCh:
				if !ok {
					if sink != nil && len(merged) > 0 {
						sink(merger.Build(first, merged))
					}
					return
				}
				if next == nil {
					continue
				}
				if nextData, ok := merger.ExtractData(next); ok && merger.CanMerge(first, next) {
					merged = append(merged, nextData...)
					if len(merged) >= merger.MaxPending {
						if sink != nil && len(merged) > 0 {
							sink(merger.Build(first, merged))
						}
						merged = nil
					}
					continue
				}
				if sink != nil && len(merged) > 0 {
					sink(merger.Build(first, merged))
				}
				if sink != nil {
					sink(next)
				}
				goto nextMsg
			default:
				if sink != nil && len(merged) > 0 {
					sink(merger.Build(first, merged))
				}
				goto nextMsg
			}
		}
	nextMsg:
	}
}
