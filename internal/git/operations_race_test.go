package git

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
)

func TestRunGitCommandIgnoresPostExitCancellation(t *testing.T) {
	stubOps(t)
	if runtime.GOOS == "windows" {
		t.Skip("shell exit command is unix-specific")
	}

	ctx, cancel := context.WithCancel(context.Background())
	ops.runGitCommandAfterWaitHook = cancel

	cmd := exec.Command("sh", "-c", "exit 0")
	killedByContext, err := runGitCommand(ctx, cmd)
	if killedByContext {
		t.Fatal("expected process to exit before any context kill")
	}
	if err != nil {
		t.Fatalf("expected successful exit to ignore post-wait cancellation, got %v", err)
	}
}
