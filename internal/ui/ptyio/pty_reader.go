package ptyio

import (
	"io"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/safego"
)

const ptyIdleHeartbeatInterval = time.Second

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

	dataCh := make(chan []byte, cfg.ReadQueueSize)
	errCh := make(chan error, 1)

	safego.Go(cfg.Label, func() {
		buf := make([]byte, cfg.ReadBufferSize)
		for {
			n, err := r.Read(buf)
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				close(dataCh)
				return
			}
			if n == 0 {
				continue
			}
			beat()
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			select {
			case dataCh <- chunk:
			case <-cancel:
				return
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
	var stoppedErr error

	for {
		select {
		case <-cancel:
			return
		case err := <-errCh:
			beat()
			stoppedErr = err
		case data, ok := <-dataCh:
			beat()
			if !ok {
				if len(pending) > 0 {
					if !SendPTYMsg(msgCh, cancel, factory.Output(pending)) {
						return
					}
				}
				if stoppedErr == nil {
					stoppedErr = io.EOF
				}
				SendPTYMsg(msgCh, cancel, factory.Stopped(stoppedErr))
				return
			}
			pending = append(pending, data...)
			startFlushTicker()
			if len(pending) >= cfg.MaxPendingBytes {
				if !SendPTYMsg(msgCh, cancel, factory.Output(pending)) {
					return
				}
				pending = nil
				if stoppedErr == nil {
					stopFlushTicker()
				}
			}
			if stoppedErr != nil && len(pending) == 0 {
				SendPTYMsg(msgCh, cancel, factory.Stopped(stoppedErr))
				return
			}
		case <-flushTick:
			beat()
			if len(pending) > 0 {
				if !SendPTYMsg(msgCh, cancel, factory.Output(pending)) {
					return
				}
				pending = nil
			}
			if len(pending) == 0 {
				stopFlushTicker()
			}
			if stoppedErr != nil {
				SendPTYMsg(msgCh, cancel, factory.Stopped(stoppedErr))
				return
			}
		case <-heartbeatTicker.C:
			beat()
		}
	}
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

		merged := make([]byte, len(data))
		copy(merged, data)
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
