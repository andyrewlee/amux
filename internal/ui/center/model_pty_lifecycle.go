package center

import (
	"io"

	tea "charm.land/bubbletea/v2"

	appPty "github.com/andyrewlee/amux/internal/pty"
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
	tab.State.StartReader(&tab.mu, ptyio.StartReaderOptionsFor(
		ptyio.ReaderNamespace{
			LabelPrefix:     "center",
			ReadQueueSize:   ptyReadQueueSize,
			MaxPendingBytes: ptyMaxPendingBytes,
		},
		func() io.Reader {
			if tab.Agent == nil || tab.Agent.Terminal == nil || tab.Agent.Terminal.IsClosed() {
				return nil
			}
			return tab.Agent.Terminal
		},
		ptyio.PTYMsgFactory{
			Output:  func(data []byte) tea.Msg { return PTYOutput{WorkspaceID: wtID, TabID: tabID, Data: data} },
			Stopped: func(err error) tea.Msg { return PTYStopped{WorkspaceID: wtID, TabID: tabID, Err: err} },
		},
		m.forwardPTYMsgs,
	))
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
	ptyRows, ptyCols, ok := appPty.WinsizeFromInts(rows, cols)
	if !ok {
		return
	}
	_ = tab.Agent.Terminal.SetSize(ptyRows, ptyCols)
	tab.ptyRows = rows
	tab.ptyCols = cols
}

func (m *Model) stopPTYReader(tab *Tab) {
	if tab == nil {
		return
	}
	tab.State.StopReader(&tab.mu)
}
