package center

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
)

func nextChatCursorRefreshDelayLocked(tab *Tab, now time.Time) (time.Duration, bool) {
	if tab == nil {
		return 0, false
	}

	var (
		delay   time.Duration
		pending bool
	)

	if !tab.lastPromptInputAt.IsZero() {
		if remaining := localInputEchoSuppressWindow - now.Sub(tab.lastPromptInputAt); remaining > 0 {
			delay = remaining
			pending = true
		}
	}

	if isTabVisiblyActiveLocked(tab, now) {
		if remaining := tabActiveWindow - now.Sub(tab.lastVisibleOutput); remaining > 0 &&
			(!pending || remaining < delay) {
			delay = remaining
			pending = true
		}
	}
	if isTabCursorOutputActiveLocked(tab, now) {
		if remaining := tabActiveWindow - now.Sub(tab.lastOutputAt); remaining > 0 &&
			(!pending || remaining < delay) {
			delay = remaining
			pending = true
		}
	}

	return delay, pending
}

func invalidateCursorSnapshotCacheLocked(tab *Tab) {
	if tab == nil {
		return
	}
	tab.cachedSnap = nil
	tab.cachedVersion = 0
	tab.cachedShowCursor = false
	tab.cachedRecentLocalInput = false
	tab.cachedRestrictCursor = false
}

func resetChatCursorActivityStateLocked(tab *Tab) {
	if tab == nil {
		return
	}
	invalidateCursorSnapshotCacheLocked(tab)
	tab.cursorRefreshGen = 0
	tab.cursorRefreshPending = false
	tab.cursorRefreshAt = time.Time{}
	tab.stableCursorSet = false
	tab.stableCursorX = 0
	tab.stableCursorY = 0
	tab.stableCursorVersion = 0
	tab.lastRestrictedVersion = 0
	tab.pendingIdleCursorRelearn = false
	tab.lastOutputAt = time.Time{}
	tab.lastUserInputAt = time.Time{}
	tab.lastPromptInputAt = time.Time{}
	tab.lastPromptSubmitAt = time.Time{}
	tab.pendingSubmitPasteEcho = ""
	tab.lastVisibleOutput = time.Time{}
	tab.bootstrapActivity = false
	tab.bootstrapLastOutputAt = time.Time{}
}

func (m *Model) scheduleChatCursorRefreshLocked(tab *Tab, workspaceID string, now time.Time) tea.Cmd {
	if tab == nil {
		return nil
	}

	if workspaceID == "" || tab.Terminal == nil || tab.Terminal.AltScreen || !m.isChatTabLocked(tab) {
		return nil
	}

	delay, pending := nextChatCursorRefreshDelayLocked(tab, now)
	if !pending {
		if tab.cursorRefreshPending {
			tab.cursorRefreshGen++
			tab.cursorRefreshPending = false
			tab.cursorRefreshAt = time.Time{}
		}
		return nil
	}
	if delay < time.Millisecond {
		delay = time.Millisecond
	}
	due := now.Add(delay)
	if tab.cursorRefreshPending && !tab.cursorRefreshAt.IsZero() && !due.Before(tab.cursorRefreshAt) {
		return nil
	}

	tab.cursorRefreshGen++
	gen := tab.cursorRefreshGen
	tab.cursorRefreshPending = true
	tab.cursorRefreshAt = due

	tabID := tab.ID
	return common.SafeTick(delay, func(time.Time) tea.Msg {
		return PTYCursorRefresh{WorkspaceID: workspaceID, TabID: tabID, Gen: gen}
	})
}

func (m *Model) scheduleChatCursorRefresh(tab *Tab, workspaceID string, now time.Time) tea.Cmd {
	if tab == nil {
		return nil
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	return m.scheduleChatCursorRefreshLocked(tab, workspaceID, now)
}

func (m *Model) updatePTYCursorRefresh(msg PTYCursorRefresh) tea.Cmd {
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil || tab.isClosed() {
		return nil
	}
	if !m.isChatTab(tab) {
		tab.mu.Lock()
		invalidateCursorSnapshotCacheLocked(tab)
		tab.mu.Unlock()
		return nil
	}

	tab.mu.Lock()
	defer tab.mu.Unlock()

	if msg.Gen == 0 {
		// Gen==0 is an explicit "invalidate now and re-arm from current state"
		// request used by immediate input/output paths. Timed refresh ticks always
		// carry a non-zero generation from scheduleChatCursorRefreshLocked.
		invalidateCursorSnapshotCacheLocked(tab)
		return m.scheduleChatCursorRefreshLocked(tab, msg.WorkspaceID, time.Now())
	}
	if msg.Gen != tab.cursorRefreshGen {
		return nil
	}

	tab.cursorRefreshPending = false
	tab.cursorRefreshAt = time.Time{}
	invalidateCursorSnapshotCacheLocked(tab)
	return m.scheduleChatCursorRefreshLocked(tab, msg.WorkspaceID, time.Now())
}
