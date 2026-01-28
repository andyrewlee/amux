package center

import (
	"io"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/safego"
)

// PTY constants
const (
	ptyFlushQuiet       = 4 * time.Millisecond
	ptyFlushMaxInterval = 16 * time.Millisecond
	ptyFlushQuietAlt    = 8 * time.Millisecond
	ptyFlushMaxAlt      = 32 * time.Millisecond
	// Inactive tabs still need to advance their terminal state, but can flush less frequently.
	ptyFlushInactiveMultiplier = 4
	ptyFlushMonitorMultiplier  = 6
	ptyFlushChunkSize          = 32 * 1024
	ptyReadBufferSize          = 32 * 1024
	ptyReadQueueSize           = 64
	ptyFrameInterval           = time.Second / 60
	ptyMaxPendingBytes         = 512 * 1024
	ptyMaxBufferedBytes        = 8 * 1024 * 1024
	ptyReaderStallTimeout      = 10 * time.Second
	tabActorStallTimeout       = 10 * time.Second
	ptyRestartMax              = 5
	ptyRestartWindow           = time.Minute

	// Backpressure thresholds (inspired by tmux's TTY_BLOCK_START/STOP)
	// When pending output exceeds this, we throttle rendering frequency
	ptyBackpressureMultiplier = 8 // threshold = multiplier * width * height
	ptyBackpressureFlushMin   = 32 * time.Millisecond
)

// PTYOutput is a message containing PTY output data
type PTYOutput struct {
	WorkspaceID string
	TabID       TabID
	Data        []byte
}

// PTYTick triggers a PTY read
type PTYTick struct {
	WorkspaceID string
	TabID       TabID
}

// PTYFlush applies buffered PTY output for a tab.
type PTYFlush struct {
	WorkspaceID string
	TabID       TabID
}

// PTYStopped signals that the PTY read loop has stopped (terminal closed or error)
type PTYStopped struct {
	WorkspaceID string
	TabID       TabID
	Err         error
	ExitCode    int
}

// PTYRestart requests restarting a PTY reader for a tab.
type PTYRestart struct {
	WorkspaceID string
	TabID       TabID
}

type selectionScrollTick struct {
	WorkspaceID string
	TabID       TabID
	Gen         uint64
}

func (m *Model) flushTiming(tab *Tab, active bool) (time.Duration, time.Duration) {
	quiet := ptyFlushQuiet
	maxInterval := ptyFlushMaxInterval

	tab.mu.Lock()
	// Only use slower Alt timing for true AltScreen mode (full-screen TUIs).
	// SyncActive (DEC 2026) already handles partial updates via screen snapshots,
	// so we don't need slower flush timing - it just makes streaming text feel laggy.
	if tab.Terminal != nil && tab.Terminal.AltScreen {
		quiet = ptyFlushQuietAlt
		maxInterval = ptyFlushMaxAlt
	}

	// Apply backpressure when pending output exceeds threshold
	// This prevents renderer thrashing during heavy output (like builds)
	if tab.Terminal != nil && len(tab.pendingOutput) > 0 {
		threshold := ptyBackpressureMultiplier * tab.Terminal.Width * tab.Terminal.Height
		if len(tab.pendingOutput) > threshold {
			// Under backpressure: use minimum flush interval
			if quiet < ptyBackpressureFlushMin {
				quiet = ptyBackpressureFlushMin
			}
			if maxInterval < ptyBackpressureFlushMin {
				maxInterval = ptyBackpressureFlushMin
			}
		}
	}
	tab.mu.Unlock()

	if !active {
		quiet *= ptyFlushInactiveMultiplier
		maxInterval *= ptyFlushInactiveMultiplier
		if maxInterval < quiet {
			maxInterval = quiet
		}
	}
	if m.monitorMode && !active {
		quiet *= ptyFlushMonitorMultiplier
		maxInterval *= ptyFlushMonitorMultiplier
		if maxInterval < quiet {
			maxInterval = quiet
		}
	}

	return quiet, maxInterval
}

func (m *Model) forwardPTYMsgs(msgCh <-chan tea.Msg) {
	for msg := range msgCh {
		if msg == nil {
			continue
		}
		out, ok := msg.(PTYOutput)
		if !ok {
			if m.msgSink != nil {
				m.msgSink(msg)
			}
			continue
		}

		merged := out
		for {
			select {
			case next, ok := <-msgCh:
				if !ok {
					if m.msgSink != nil && len(merged.Data) > 0 {
						m.msgSink(merged)
					}
					return
				}
				if next == nil {
					continue
				}
				if nextOut, ok := next.(PTYOutput); ok &&
					nextOut.WorkspaceID == merged.WorkspaceID &&
					nextOut.TabID == merged.TabID {
					merged.Data = append(merged.Data, nextOut.Data...)
					if len(merged.Data) >= ptyMaxPendingBytes {
						if m.msgSink != nil && len(merged.Data) > 0 {
							m.msgSink(merged)
						}
						merged.Data = nil
					}
					continue
				}
				if m.msgSink != nil && len(merged.Data) > 0 {
					m.msgSink(merged)
				}
				if m.msgSink != nil {
					m.msgSink(next)
				}
				goto nextMsg
			default:
				if m.msgSink != nil && len(merged.Data) > 0 {
					m.msgSink(merged)
				}
				goto nextMsg
			}
		}
	nextMsg:
	}
}

func runPTYReader(term *appPty.Terminal, msgCh chan tea.Msg, cancel <-chan struct{}, wtID string, tabID TabID, heartbeat *int64) {
	// Ensure msgCh is always closed even if we panic, so forwardPTYMsgs doesn't block forever.
	// The inner recover() catches double-close panics from existing close(msgCh) calls.
	defer func() {
		defer func() { _ = recover() }()
		close(msgCh)
	}()

	if term == nil {
		return
	}
	beat := func() {
		if heartbeat != nil {
			atomic.StoreInt64(heartbeat, time.Now().UnixNano())
		}
	}
	beat()

	dataCh := make(chan []byte, ptyReadQueueSize)
	errCh := make(chan error, 1)

	safego.Go("center.pty_read_loop", func() {
		buf := make([]byte, ptyReadBufferSize)
		for {
			n, err := term.Read(buf)
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

	ticker := time.NewTicker(ptyFrameInterval)
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
					if !sendPTYMsg(msgCh, cancel, PTYOutput{WorkspaceID: wtID, TabID: tabID, Data: pending}) {
						close(msgCh)
						return
					}
				}
				if stoppedErr == nil {
					stoppedErr = io.EOF
				}
				sendPTYMsg(msgCh, cancel, PTYStopped{WorkspaceID: wtID, TabID: tabID, Err: stoppedErr})
				close(msgCh)
				return
			}
			pending = append(pending, data...)
			if len(pending) >= ptyMaxPendingBytes {
				if !sendPTYMsg(msgCh, cancel, PTYOutput{WorkspaceID: wtID, TabID: tabID, Data: pending}) {
					close(msgCh)
					return
				}
				pending = nil
			}
		case <-ticker.C:
			beat()
			if len(pending) > 0 {
				if !sendPTYMsg(msgCh, cancel, PTYOutput{WorkspaceID: wtID, TabID: tabID, Data: pending}) {
					close(msgCh)
					return
				}
				pending = nil
			}
			if stoppedErr != nil {
				sendPTYMsg(msgCh, cancel, PTYStopped{WorkspaceID: wtID, TabID: tabID, Err: stoppedErr})
				close(msgCh)
				return
			}
		}
	}
}

func sendPTYMsg(msgCh chan tea.Msg, cancel <-chan struct{}, msg tea.Msg) bool {
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

func (m *Model) startPTYReader(wtID string, tab *Tab) tea.Cmd {
	if tab == nil {
		return nil
	}
	if tab.isClosed() {
		return nil
	}
	if !tab.Running {
		return nil
	}
	tab.mu.Lock()
	if tab.readerActive {
		if tab.ptyMsgCh == nil || tab.readerCancel == nil {
			tab.readerActive = false
		} else {
			tab.mu.Unlock()
			return nil
		}
	}
	if tab.Agent == nil || tab.Agent.Terminal == nil || tab.Agent.Terminal.IsClosed() {
		tab.readerActive = false
		tab.mu.Unlock()
		return nil
	}
	tab.readerActive = true
	tab.ptyRestartBackoff = 0
	atomic.StoreInt64(&tab.ptyHeartbeat, time.Now().UnixNano())

	if tab.readerCancel != nil {
		safeClose(tab.readerCancel)
	}
	tab.readerCancel = make(chan struct{})
	tab.ptyMsgCh = make(chan tea.Msg, ptyReadQueueSize)

	term := tab.Agent.Terminal
	tabID := tab.ID
	cancel := tab.readerCancel
	msgCh := tab.ptyMsgCh
	tab.mu.Unlock()

	safego.Go("center.pty_reader", func() {
		defer m.markPTYReaderStopped(tab)
		runPTYReader(term, msgCh, cancel, wtID, tabID, &tab.ptyHeartbeat)
	})
	safego.Go("center.pty_forward", func() {
		m.forwardPTYMsgs(msgCh)
	})
	return nil
}

func safeClose(ch chan struct{}) {
	defer func() {
		_ = recover()
	}()
	close(ch)
}

func (m *Model) resizePTY(tab *Tab, rows, cols int) {
	if tab == nil || tab.Agent == nil || tab.Agent.Terminal == nil {
		return
	}
	if rows < 1 || cols < 1 {
		return
	}
	if tab.ptyRows == rows && tab.ptyCols == cols {
		return
	}
	_ = tab.Agent.Terminal.SetSize(uint16(rows), uint16(cols))
	tab.ptyRows = rows
	tab.ptyCols = cols
}

func (m *Model) stopPTYReader(tab *Tab) {
	if tab == nil {
		return
	}
	tab.mu.Lock()
	if tab.readerCancel != nil {
		safeClose(tab.readerCancel)
		tab.readerCancel = nil
	}
	tab.readerActive = false
	tab.ptyMsgCh = nil
	tab.mu.Unlock()
	atomic.StoreInt64(&tab.ptyHeartbeat, 0)
}

func (m *Model) markPTYReaderStopped(tab *Tab) {
	if tab == nil {
		return
	}
	tab.mu.Lock()
	tab.readerActive = false
	tab.ptyMsgCh = nil
	tab.mu.Unlock()
	atomic.StoreInt64(&tab.ptyHeartbeat, 0)
}
