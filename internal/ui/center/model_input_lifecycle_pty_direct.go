package center

import (
	"time"

	"github.com/andyrewlee/amux/internal/safego"
)

var ptyDirectFlushRetryInterval = ptyFlushMaxInterval

func (m *Model) emitDirectPTYFlush(workspaceID string, tab *Tab) {
	if tab == nil || m.msgSink == nil {
		return
	}
	m.msgSink(PTYFlush{WorkspaceID: workspaceID, TabID: tab.ID})

	tab.mu.Lock()
	if tab.directFlushRetryArmed {
		tab.mu.Unlock()
		return
	}
	tab.directFlushRetryArmed = true
	tab.mu.Unlock()

	safego.Go("center.pty_direct_flush_retry", func() {
		ticker := time.NewTicker(ptyDirectFlushRetryInterval)
		defer ticker.Stop()
		for range ticker.C {
			if tab.isClosed() {
				tab.mu.Lock()
				tab.directFlushRetryArmed = false
				tab.mu.Unlock()
				return
			}

			tab.mu.Lock()
			pending := tab.pendingOutput.Len() > 0
			scheduled := tab.flushScheduled
			tabID := tab.ID
			if !pending || !scheduled {
				tab.directFlushRetryArmed = false
				tab.mu.Unlock()
				return
			}
			tab.mu.Unlock()

			if m.msgSink != nil {
				m.msgSink(PTYFlush{WorkspaceID: workspaceID, TabID: tabID})
			}
		}
	})
}
