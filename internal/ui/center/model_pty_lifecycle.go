package center

import (
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

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
	if tab.ReaderActive {
		if tab.MsgCh == nil || tab.ReaderCancel == nil {
			tab.ReaderActive = false
		} else {
			tab.mu.Unlock()
			return nil
		}
	}
	if tab.Agent == nil || tab.Agent.Terminal == nil || tab.Agent.Terminal.IsClosed() {
		tab.ReaderActive = false
		tab.mu.Unlock()
		return nil
	}
	tab.ReaderActive = true
	tab.RestartBackoff = 0
	atomic.StoreInt64(&tab.Heartbeat, time.Now().UnixNano())

	if tab.ReaderCancel != nil {
		close(tab.ReaderCancel)
	}
	tab.ReaderCancel = make(chan struct{})
	tab.MsgCh = make(chan tea.Msg, ptyReadQueueSize)

	term := tab.Agent.Terminal
	tabID := tab.ID
	cancel := tab.ReaderCancel
	msgCh := tab.MsgCh
	tab.mu.Unlock()

	safego.Go("center.pty_reader", func() {
		defer m.markPTYReaderStopped(tab)
		ptyio.RunPTYReader(term, msgCh, cancel, &tab.Heartbeat, ptyio.PTYReaderConfig{
			Label:           "center.pty_read_loop",
			ReadBufferSize:  ptyReadBufferSize,
			ReadQueueSize:   ptyReadQueueSize,
			FrameInterval:   ptyFrameInterval,
			MaxPendingBytes: ptyMaxPendingBytes,
		}, ptyio.PTYMsgFactory{
			Output:  func(data []byte) tea.Msg { return PTYOutput{WorkspaceID: wtID, TabID: tabID, Data: data} },
			Stopped: func(err error) tea.Msg { return PTYStopped{WorkspaceID: wtID, TabID: tabID, Err: err} },
		})
	})
	safego.Go("center.pty_forward", func() {
		m.forwardPTYMsgs(msgCh)
	})
	return nil
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
	if tab.ReaderCancel != nil {
		close(tab.ReaderCancel)
		tab.ReaderCancel = nil
	}
	tab.ReaderActive = false
	tab.MsgCh = nil
	tab.mu.Unlock()
	atomic.StoreInt64(&tab.Heartbeat, 0)
}

func (m *Model) markPTYReaderStopped(tab *Tab) {
	if tab == nil {
		return
	}
	tab.mu.Lock()
	tab.ReaderActive = false
	tab.MsgCh = nil
	tab.mu.Unlock()
	atomic.StoreInt64(&tab.Heartbeat, 0)
}
