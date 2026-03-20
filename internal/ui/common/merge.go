package common

import "github.com/andyrewlee/amux/internal/data"

// RebindWorkspace returns the workspace pointer to use after a rebind.
// If preserveRuntime is true and existing is non-nil, the existing runtime
// is preserved on a copy of current.
func RebindWorkspace(current, existing *data.Workspace, preserveRuntime bool) *data.Workspace {
	if current == nil {
		return existing
	}
	if !preserveRuntime || existing == nil {
		return current
	}
	rebound := *current
	rebound.Runtime = existing.Runtime
	return &rebound
}

// MergeByID merges two slices by unique ID, preserving existing items first,
// then appending non-duplicate incoming items. Returns the merged slice and
// the migrated index of incomingActive within the merged slice (-1 if not found).
func MergeByID[T any, ID comparable](existing, incoming []T, incomingActive int, getID func(T) ID, isNil func(T) bool) ([]T, int) {
	merged := make([]T, 0, len(existing)+len(incoming))
	indexByID := make(map[ID]int, len(existing)+len(incoming))

	for _, item := range existing {
		if isNil(item) {
			continue
		}
		id := getID(item)
		if _, ok := indexByID[id]; ok {
			continue
		}
		indexByID[id] = len(merged)
		merged = append(merged, item)
	}

	migratedActive := -1
	for i, item := range incoming {
		if isNil(item) {
			continue
		}
		id := getID(item)
		if idx, ok := indexByID[id]; ok {
			if i == incomingActive {
				migratedActive = idx
			}
			continue
		}
		indexByID[id] = len(merged)
		merged = append(merged, item)
		if i == incomingActive {
			migratedActive = len(merged) - 1
		}
	}

	return merged, migratedActive
}
