package common

import (
	"io"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/safego"
)

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
// Closes msgCh on exit.
func RunPTYReader(
	r io.Reader, msgCh chan tea.Msg, cancel <-chan struct{},
	heartbeat *int64, cfg PTYReaderConfig, factory PTYMsgFactory,
) {
	// Ensure msgCh is always closed even if we panic, so forwardPTYMsgs doesn't block forever.
	// The inner recover() catches double-close panics from existing close(msgCh) calls.
	defer func() {
		defer func() { _ = recover() }()
		close(msgCh)
	}()

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

	ticker := time.NewTicker(cfg.FrameInterval)
	defer ticker.Stop()

	var pending []byte
	var stoppedErr error

	for {
		select {
		case <-cancel:
			close(msgCh)
			return
		case err := <-errCh:
			beat()
			stoppedErr = err
		case data, ok := <-dataCh:
			beat()
			if !ok {
				if len(pending) > 0 {
					if !SendPTYMsg(msgCh, cancel, factory.Output(pending)) {
						close(msgCh)
						return
					}
				}
				if stoppedErr == nil {
					stoppedErr = io.EOF
				}
				SendPTYMsg(msgCh, cancel, factory.Stopped(stoppedErr))
				close(msgCh)
				return
			}
			pending = append(pending, data...)
			if len(pending) >= cfg.MaxPendingBytes {
				if !SendPTYMsg(msgCh, cancel, factory.Output(pending)) {
					close(msgCh)
					return
				}
				pending = nil
			}
		case <-ticker.C:
			beat()
			if len(pending) > 0 {
				if !SendPTYMsg(msgCh, cancel, factory.Output(pending)) {
					close(msgCh)
					return
				}
				pending = nil
			}
			if stoppedErr != nil {
				SendPTYMsg(msgCh, cancel, factory.Stopped(stoppedErr))
				close(msgCh)
				return
			}
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

// SafeClose closes ch, recovering from double-close panics.
func SafeClose(ch chan struct{}) {
	defer func() {
		_ = recover()
	}()
	close(ch)
}
