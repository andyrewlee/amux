package common

// TabSet tracks per-workspace tab lists and the active tab index. It is the
// shared storage + navigation core behind the center tab strip and the
// sidebar terminal tabs: both keep a map of workspaceID → tabs plus a map of
// workspaceID → active index, with circular next/prev selection.
//
// The maps are exported because both consumers iterate and mutate them
// directly in workspace-rebind and session-discovery paths; TabSet owns the
// navigation math so it is written once.
type TabSet[T any] struct {
	ByWorkspace       map[string][]T
	ActiveByWorkspace map[string]int
}

// NewTabSet returns an empty TabSet with initialized maps.
func NewTabSet[T any]() TabSet[T] {
	return TabSet[T]{
		ByWorkspace:       make(map[string][]T),
		ActiveByWorkspace: make(map[string]int),
	}
}

// Tabs returns the workspace's tabs (nil when none).
func (s *TabSet[T]) Tabs(wsID string) []T {
	return s.ByWorkspace[wsID]
}

// ActiveIdx returns the workspace's active tab index (0 when unset).
func (s *TabSet[T]) ActiveIdx(wsID string) int {
	return s.ActiveByWorkspace[wsID]
}

// SetActiveIdx records the workspace's active tab index.
func (s *TabSet[T]) SetActiveIdx(wsID string, idx int) {
	s.ActiveByWorkspace[wsID] = idx
}

// NextIdx advances the active index circularly. It reports the new index and
// whether a move happened (false when the workspace has no tabs).
func (s *TabSet[T]) NextIdx(wsID string) (int, bool) {
	tabs := s.ByWorkspace[wsID]
	if len(tabs) == 0 {
		return 0, false
	}
	idx := (s.ActiveByWorkspace[wsID] + 1) % len(tabs)
	s.ActiveByWorkspace[wsID] = idx
	return idx, true
}

// PrevIdx moves the active index back circularly. It reports the new index
// and whether a move happened (false when the workspace has no tabs).
func (s *TabSet[T]) PrevIdx(wsID string) (int, bool) {
	tabs := s.ByWorkspace[wsID]
	if len(tabs) == 0 {
		return 0, false
	}
	idx := (s.ActiveByWorkspace[wsID] - 1 + len(tabs)) % len(tabs)
	s.ActiveByWorkspace[wsID] = idx
	return idx, true
}

// SelectIdx sets the active index when it is in range, reporting success.
func (s *TabSet[T]) SelectIdx(wsID string, idx int) bool {
	if idx < 0 || idx >= len(s.ByWorkspace[wsID]) {
		return false
	}
	s.ActiveByWorkspace[wsID] = idx
	return true
}

// DeleteWorkspace drops all tab tracking for a workspace.
func (s *TabSet[T]) DeleteWorkspace(wsID string) {
	delete(s.ByWorkspace, wsID)
	delete(s.ActiveByWorkspace, wsID)
}
