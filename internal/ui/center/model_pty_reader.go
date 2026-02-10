package center

import (
	"io"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/safego"
)

func (m *Model) flushTiming(tab *Tab, active bool) (time.Duration, time.Duration) {
	quiet := ptyFlushQuiet
	maxInterval := ptyFlushMaxInterval

	pendingLen := 0
	termWidth := 0
	termHeight := 0
	altScreen := false

	tab.mu.Lock()
	// Only use slower Alt timing for true AltScreen mode (full-screen TUIs).
	// SyncActive (DEC 2026) already handles partial updates via screen snapshots,
	// so we don't need slower flush timing - it just makes streaming text feel laggy.
	if tab.Terminal != nil {
		altScreen = tab.Terminal.AltScreen
		termWidth = tab.Terminal.Width
		termHeight = tab.Terminal.Height
	}
	pendingLen = len(tab.pendingOutput)

	// Apply backpressure when pending output exceeds threshold
	// This prevents renderer thrashing during heavy output (like builds)
	tab.mu.Unlock()

	if altScreen {
		quiet = ptyFlushQuietAlt
		maxInterval = ptyFlushMaxAlt
	}

	if pendingLen > 0 && termWidth > 0 && termHeight > 0 {
		threshold := ptyBackpressureMultiplier * termWidth * termHeight
		if pendingLen > threshold {
			// Under backpressure: use minimum flush interval
			if quiet < ptyBackpressureFlushFloor {
				quiet = ptyBackpressureFlushFloor
			}
			if maxInterval < ptyBackpressureFlushFloor {
				maxInterval = ptyBackpressureFlushFloor
			}
		}
	}

	if !active {
		multiplier := ptyFlushInactiveMultiplier
		switch busyTabs := m.busyPTYTabCount(time.Now()); {
		case busyTabs >= ptyVeryHeavyLoadTabThreshold:
			multiplier = ptyFlushInactiveVeryHeavyMultiplier
		case busyTabs >= ptyHeavyLoadTabThreshold:
			multiplier = ptyFlushInactiveHeavyMultiplier
		}

		quiet *= time.Duration(multiplier)
		maxInterval *= time.Duration(multiplier)
		if quiet > ptyFlushInactiveMaxIntervalCap {
			quiet = ptyFlushInactiveMaxIntervalCap
		}
		if maxInterval > ptyFlushInactiveMaxIntervalCap {
			maxInterval = ptyFlushInactiveMaxIntervalCap
		}
		if maxInterval < quiet {
			maxInterval = quiet
		}
	}

	return quiet, maxInterval
}

func (m *Model) busyPTYTabCount(now time.Time) int {
	if !m.flushLoadSampleAt.IsZero() && now.Sub(m.flushLoadSampleAt) < ptyLoadSampleInterval {
		return m.cachedBusyTabCount
	}

	count := 0
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			if tab == nil || tab.isClosed() {
				continue
			}
			busy := atomic.LoadUint32(&tab.readerActiveState) == 1 || len(tab.pendingOutput) > 0
			if busy {
				count++
			}
		}
	}

	m.flushLoadSampleAt = now
	m.cachedBusyTabCount = count
	return count
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
