package common

// MergeByID merges incoming into existing, de-duplicating by id while preserving
// order (existing first, then new incoming items). It returns the merged slice
// and the index that incoming[incomingActive] maps to in the result (-1 if the
// active item was nil or out of range). isNil filters out nil/zero entries.
func MergeByID[T any, K comparable](existing, incoming []T, incomingActive int, id func(T) K, isNil func(T) bool) ([]T, int) {
	merged := make([]T, 0, len(existing)+len(incoming))
	indexByID := make(map[K]int, len(existing)+len(incoming))

	for _, item := range existing {
		if isNil(item) {
			continue
		}
		if _, ok := indexByID[id(item)]; ok {
			continue
		}
		indexByID[id(item)] = len(merged)
		merged = append(merged, item)
	}

	migratedActive := -1
	for i, item := range incoming {
		if isNil(item) {
			continue
		}
		if idx, ok := indexByID[id(item)]; ok {
			if i == incomingActive {
				migratedActive = idx
			}
			continue
		}
		indexByID[id(item)] = len(merged)
		merged = append(merged, item)
		if i == incomingActive {
			migratedActive = len(merged) - 1
		}
	}

	return merged, migratedActive
}

// RebindTabMaps migrates a workspace's tab slice and active-tab index from oldID
// to newID across the two per-workspace maps: it merges any tabs already present
// under newID (via MergeByID), writes the result under newID, deletes oldID from
// both maps, and clamps the surviving active index. It returns the merged slice.
//
// Callers keep their own surrounding logic (the "seen but empty" special case,
// workspace-pointer fixups, PTY restarts) and delegate only this reconciliation,
// which was byte-identical between the center and sidebar panes — so a clamp fix
// now happens once instead of being mirrored.
func RebindTabMaps[T any, K comparable](
	tabsByWorkspace map[string][]T,
	activeByWorkspace map[string]int,
	oldID, newID string,
	id func(T) K, isNil func(T) bool,
) []T {
	oldTabs := tabsByWorkspace[oldID]
	newTabs := tabsByWorkspace[newID]
	oldActive, oldActiveOK := activeByWorkspace[oldID]
	newActive, newActiveOK := activeByWorkspace[newID]
	merged, migratedActive := MergeByID(newTabs, oldTabs, oldActive, id, isNil)

	tabsByWorkspace[newID] = merged
	delete(tabsByWorkspace, oldID)
	switch {
	case oldActiveOK && (!newActiveOK || len(newTabs) == 0):
		if migratedActive < 0 {
			migratedActive = 0
		}
		if len(merged) == 0 {
			migratedActive = 0
		} else if migratedActive >= len(merged) {
			migratedActive = len(merged) - 1
		}
		activeByWorkspace[newID] = migratedActive
	case newActiveOK:
		if len(merged) == 0 {
			activeByWorkspace[newID] = 0
		} else if newActive >= len(merged) {
			activeByWorkspace[newID] = len(merged) - 1
		}
	}
	delete(activeByWorkspace, oldID)
	return merged
}
