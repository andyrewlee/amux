package computer

import (
	"context"
	"fmt"
)

// execCommand executes a command, respecting opts.Timeout if provided.
// If opts is nil or opts.Timeout is 0, no context timeout is applied
// and the provider is expected to enforce any default timeout.
func execCommand(sandbox RemoteComputer, cmd string, opts *ExecOptions) (*ExecResult, error) {
	if sandbox == nil {
		return nil, fmt.Errorf("computer is nil")
	}

	// If opts specifies a timeout, apply it to the context
	if opts != nil && opts.Timeout > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
		defer cancel()
		return sandbox.Exec(ctx, cmd, opts)
	}

	// Otherwise, let the provider handle timeout (via opts.Timeout or its own defaults)
	return sandbox.Exec(context.Background(), cmd, opts)
}
