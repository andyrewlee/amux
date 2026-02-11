package cli

import (
	"context"
	"os"
	"os/exec"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func isReadable(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// tmuxNewSession builds a tmux new-session command using the same server/config
// options as other tmux helpers. This avoids importing unexported functions.
func tmuxNewSession(opts tmux.Options, extraArgs ...string) (*exec.Cmd, context.CancelFunc) {
	args := []string{}
	if opts.ServerName != "" {
		args = append(args, "-L", opts.ServerName)
	}
	if opts.ConfigPath != "" {
		args = append(args, "-f", opts.ConfigPath)
	}
	args = append(args, extraArgs...)

	timeout := 5 * time.Second
	if opts.CommandTimeout > 0 {
		timeout = opts.CommandTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	cmd := exec.CommandContext(ctx, "tmux", args...)
	return cmd, cancel
}
