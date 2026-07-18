//go:build !windows

package process

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// snapshotTimeout bounds the ps invocation: under heavy system load (the
// exact condition teardown/reaping runs in) a stuck ps must not wedge the
// caller.
const snapshotTimeout = 10 * time.Second

// Snapshot lists live processes (pid, pgid, ppid, cpu, start time, command
// line) via ps. Processes the current user cannot signal may appear; kills on
// them fail benignly later rather than being filtered here. LC_ALL=C pins the
// lstart column to the layout parsePSLines expects.
func Snapshot() ([]ProcessInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), snapshotTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ps", "-axo", "pid=,pgid=,ppid=,pcpu=,lstart=,command=")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ps snapshot: %w", err)
	}
	return parsePSLines(string(out)), nil
}
