package sidebar

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func (m *TerminalModel) forwardPTYMsgs(msgCh <-chan tea.Msg) {
	common.ForwardPTYMsgs(msgCh, m.msgSink, common.OutputMerger{
		ExtractData: func(msg tea.Msg) ([]byte, bool) {
			if out, ok := msg.(messages.SidebarPTYOutput); ok {
				return out.Data, true
			}
			return nil, false
		},
		CanMerge: func(cur, next tea.Msg) bool {
			c, _ := cur.(messages.SidebarPTYOutput)
			n, _ := next.(messages.SidebarPTYOutput)
			return c.WorkspaceID == n.WorkspaceID && c.TabID == n.TabID
		},
		Build: func(first tea.Msg, data []byte) tea.Msg {
			out, _ := first.(messages.SidebarPTYOutput)
			out.Data = data
			return out
		},
		MaxPending: ptyMaxPendingBytes,
	})
}
