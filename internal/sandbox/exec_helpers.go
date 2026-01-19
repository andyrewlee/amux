package sandbox

import (
	"context"
	"fmt"
)

// execCommand executes a command, respecting opts.Timeout if provided.
// If opts is nil or opts.Timeout is 0, no context timeout is applied
// and the provider is expected to enforce any default timeout.
func execCommand(computer RemoteSandbox, cmd string, opts *ExecOptions) (*ExecResult, error) {
	if computer == nil {
		return nil, fmt.Errorf("sandbox is nil")
	}

	// If opts specifies a timeout, apply it to the context
	if opts != nil && opts.Timeout > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
		defer cancel()
		return computer.Exec(ctx, cmd, opts)
	}

	// Otherwise, let the provider handle timeout (via opts.Timeout or its own defaults)
	return computer.Exec(context.Background(), cmd, opts)
}
