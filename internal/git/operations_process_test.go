package git

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestGitWrappersCanceledContextReturnsPromptly exercises all three public
// wrappers against an already-canceled context: none may spawn git, block on
// pipes, or misreport the cancellation.
func TestGitWrappersCanceledContextReturnsPromptly(t *testing.T) {
	repo := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	calls := map[string]func() error{
		"RunGitCtx": func() error {
			_, err := RunGitCtx(ctx, repo, "status")
			return err
		},
		"RunGitAllowFailureCtx": func() error {
			_, err := RunGitAllowFailureCtx(ctx, repo, "status")
			return err
		},
		"RunGitRawCtx": func() error {
			_, err := RunGitRawCtx(ctx, repo, "status")
			return err
		},
	}
	for name, fn := range calls {
		if err := fn(); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s error = %v, want wrapped context.Canceled", name, err)
		}
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("canceled wrappers took %v, want prompt return", elapsed)
	}
}
