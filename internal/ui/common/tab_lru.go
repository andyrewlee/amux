package common

import (
	"sort"
	"time"
)

// LRUTabCandidate is one auto-detach candidate for attached-PTY limit
// enforcement, shared by the center agent-tab and sidebar terminal-tab
// limiters so both panes evict in the same order.
type LRUTabCandidate[P any] struct {
	WorkspaceID string
	Index       int
	LastActive  time.Time
	Payload     P
}

// SortLRUTabCandidates orders candidates least-recently-active first. The
// zero time naturally sorts before every real timestamp, so tabs with
// unknown recency are evicted first. Ties break by workspace ID, then tab
// index, keeping eviction deterministic.
func SortLRUTabCandidates[P any](candidates []LRUTabCandidate[P]) {
	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.LastActive.Equal(right.LastActive) {
			if left.WorkspaceID == right.WorkspaceID {
				return left.Index < right.Index
			}
			return left.WorkspaceID < right.WorkspaceID
		}
		return left.LastActive.Before(right.LastActive)
	})
}
