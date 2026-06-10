package center

import (
	"io"

	tea "charm.land/bubbletea/v2"

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
	tabID := tab.ID
	tab.State.StartReader(&tab.mu, ptyio.StartReaderOptions{
		AcquireTerm: func() io.Reader {
			if tab.Agent == nil || tab.Agent.Terminal == nil || tab.Agent.Terminal.IsClosed() {
				return nil
			}
			return tab.Agent.Terminal
		},
		Config: ptyio.PTYReaderConfig{
			Label:           "center.pty_read_loop",
			ReadBufferSize:  ptyReadBufferSize,
			ReadQueueSize:   ptyReadQueueSize,
			FrameInterval:   ptyFrameInterval,
			MaxPendingBytes: ptyMaxPendingBytes,
		},
		Factory: ptyio.PTYMsgFactory{
			Output:  func(data []byte) tea.Msg { return PTYOutput{WorkspaceID: wtID, TabID: tabID, Data: data} },
			Stopped: func(err error) tea.Msg { return PTYStopped{WorkspaceID: wtID, TabID: tabID, Err: err} },
		},
		ReaderLabel:  "center.pty_reader",
		ForwardLabel: "center.pty_forward",
		Forward:      m.forwardPTYMsgs,
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
	tab.State.StopReader(&tab.mu)
}

func (m *Model) markPTYReaderStopped(tab *Tab) {
	if tab == nil {
		return
	}
	tab.State.MarkReaderStopped(&tab.mu)
}
