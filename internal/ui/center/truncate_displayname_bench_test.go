package center

import "testing"

// Plan 046 measurement: truncateDisplayName runs at tab-creation time only
// (DisplayName is stored on the Tab), so this benchmark exists to put a number
// on the per-call cost — not to drive a rewrite.
func BenchmarkTruncateDisplayName(b *testing.B) {
	names := []string{
		"claude",
		"a-very-long-branch-name-feature/sanitize-repo-text-render-surfaces",
		"Diff: internal/ui/center/model_input_lifecycle_pty_restart.go",
	}
	for b.Loop() {
		for _, n := range names {
			_ = truncateDisplayName(n)
		}
	}
}
