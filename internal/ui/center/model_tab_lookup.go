package center

import "github.com/andyrewlee/amux/internal/ui/ptyio"

// resolveTabForResult finds the tab an async PTY result belongs to, preferring
// the workspace the result was stamped with and falling back to an ID-only
// scan. The returned workspace ID is the key the tab is actually filed under,
// which is the one follow-up messages must use.
//
// Closed tabs never satisfy the lookup: a result arriving after close must not
// resurrect them. The sidebar resolver omits that filter because TerminalTab
// has no closed state — closed terminals leave the map entirely.
func (m *Model) resolveTabForResult(wsID string, tabID TabID, context string) (*Tab, string) {
	return ptyio.ResolveKeyedTab(m.tabs.ByWorkspace, wsID, tabID,
		func(t *Tab) TabID { return t.ID },
		func(t *Tab) bool { return t != nil && !t.isClosed() },
		context, "tab")
}
