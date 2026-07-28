package center

import "github.com/andyrewlee/amux/internal/logging"

// findTabByIDAnyWorkspace returns the tab with the given ID regardless of which
// workspace key it is filed under, plus that key.
//
// TabIDs are process-unique (generateTabID is a per-instance counter), so the
// ID alone is enough to identify a tab. The workspace key is only a routing
// hint, and it can go stale: a tab's Workspace pointer is what stamps the
// WorkspaceID on an async result, so any rebind or identity change between
// dispatch and delivery makes the pair (wsID, tabID) miss while the tab is
// still very much alive. Losing an async result that way used to strand the
// tab, so lookups that must not miss fall back to this scan.
func (m *Model) findTabByIDAnyWorkspace(tabID TabID) (*Tab, string) {
	for wsID, tabs := range m.tabs.ByWorkspace {
		for _, tab := range tabs {
			if tab == nil || tab.isClosed() {
				continue
			}
			if tab.ID == tabID {
				return tab, wsID
			}
		}
	}
	return nil, ""
}

// resolveTabForResult finds the tab an async PTY result belongs to, preferring
// the workspace the result was stamped with and falling back to an ID-only
// scan. The returned workspace ID is the key the tab is actually filed under,
// which is the one follow-up messages must use.
func (m *Model) resolveTabForResult(wsID string, tabID TabID, context string) (*Tab, string) {
	if tab := m.getTabByID(wsID, tabID); tab != nil {
		return tab, wsID
	}
	tab, actualWsID := m.findTabByIDAnyWorkspace(tabID)
	if tab == nil {
		return nil, ""
	}
	// Worth a warning: the result was routed with a workspace ID the tab is no
	// longer filed under. The fallback keeps the user unblocked, but the drift
	// itself is a bug worth seeing in the log.
	logging.Warn("%s: tab %s routed with workspace %s but filed under %s", context, tabID, wsID, actualWsID)
	return tab, actualWsID
}
