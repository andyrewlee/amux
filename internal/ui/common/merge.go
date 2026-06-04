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
