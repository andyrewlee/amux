package cli

import (
	"testing"
	"time"
)

func TestIsTaskProgressOnlyLine_PreservesAssistantTryQuotedContent(t *testing.T) {
	line := `Try "go test ./internal/cli" to verify the fix.`
	if isTaskProgressOnlyLine(line) {
		t.Fatalf("isTaskProgressOnlyLine(%q) = true, want false", line)
	}
}

func TestTaskStatusLooksComplete_TryQuotedSummaryCountsAsOutput(t *testing.T) {
	candidate := taskAgentCandidate{
		CreatedAt:     time.Now().Add(-2 * time.Minute).Unix(),
		LastOutputAt:  time.Now().Add(-2 * time.Minute),
		HasLastOutput: true,
	}
	snap := taskAgentSnapshot{
		Summary:    `Try "go test ./internal/cli" to verify the fix.`,
		LatestLine: "Done.",
		NeedsInput: false,
	}
	if !taskStatusLooksComplete(candidate, snap) {
		t.Fatalf("taskStatusLooksComplete() = false, want true")
	}
}
