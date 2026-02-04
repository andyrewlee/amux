package center

import (
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/safego"
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
