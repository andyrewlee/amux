//go:build !windows

package panelaunch

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// execFn is the exec seam: tests observe argv/env and the pre-exec
// filesystem state without replacing the process.
var execFn = syscall.Exec

// runPayload consumes a payload: validate the path shape and private
// ownership, read and unlink the payload before trusting its contents,
// merge the captured assignments over the pane's inherited environment,
// chdir to the captured working directory, then replace this process with
// `sh -lc <command>` resolved against the merged PATH. syscall.Exec keeps
// the pane PID, PTY identity, signal disposition, and exit status exactly
// as if tmux had spawned the shell directly.
func runPayload(path string) int {
	dir, err := validateInvocationPath(path)
	if err != nil {
		return stageFail(stageInvocation)
	}
	if err := checkAttemptDir(dir); err != nil {
		return stageFail(stageOwnership)
	}
	f, err := openPayload(dir)
	if err != nil {
		return stageFail(stageOwnership)
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxPayloadBytes))
	_ = f.Close()
	if err != nil {
		// Ownership is proven; a half-read payload must not be replayed.
		_ = removeOwnedPayload(dir)
		_ = os.Remove(dir)
		return stageFail(stagePayload)
	}
	// Consume before decoding: whatever the payload says, it is used once.
	_ = removeOwnedPayload(dir)
	_ = os.Remove(dir) // rmdir only succeeds when the attempt is empty

	p, err := decodePayload(raw, nowFn())
	if err != nil {
		if serr, ok := err.(stageError); ok {
			return stageFail(serr)
		}
		return stageFail(stagePayload)
	}
	env := dropEnvKeys(mergeEnv(os.Environ(), p.Environment), "TMUX", "TMUX_PANE")
	if err := syscall.Chdir(string(p.WorkDir)); err != nil {
		return stageFail(stageDirectory)
	}
	sh, err := resolveShell(env)
	if err != nil {
		return stageFail(stageShell)
	}
	if err := execFn(sh, []string{"sh", "-lc", string(p.Command)}, env); err != nil {
		return stageFail(stageExec)
	}
	return 0 // unreachable: exec replaces the process
}

// discardPayload removes an attempt the launcher proved unused (an already
// present session, or a lost create race with a proven foreign winner). The
// same ownership boundary applies and an already-consumed attempt is a
// harmless no-op.
func discardPayload(path string) int {
	dir, err := validateInvocationPath(path)
	if err != nil {
		return stageFail(stageInvocation)
	}
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return 0 // attempt already fully consumed
	}
	if err := checkAttemptDir(dir); err != nil {
		return stageFail(stageOwnership)
	}
	if err := removeOwnedPayload(dir); err != nil {
		return stageFail(stageOwnership)
	}
	_ = os.Remove(dir) // non-empty attempts are left for the sweep
	return 0
}

// resolveShell finds `sh` the way the previous `env` delivery resolved it:
// search the merged PATH, where a missing PATH falls back to the execvp
// default and empty or relative elements resolve against the post-chdir
// working directory. exec.LookPath would use the parent environment and its
// ErrDot policy instead, and a hardcoded /bin/sh would bypass an env layer
// that intentionally shadows sh.
func resolveShell(env []string) (string, error) {
	pathVal, found := "", false
	for _, e := range env {
		if name, value, ok := strings.Cut(e, "="); ok && name == "PATH" {
			pathVal, found = value, true
		}
	}
	if !found {
		pathVal = "/bin:/usr/bin"
	}
	for _, dir := range strings.Split(pathVal, ":") {
		candidate := "sh"
		if dir != "" {
			candidate = dir + string(filepath.Separator) + "sh"
		}
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		return candidate, nil
	}
	return "", errors.New("no executable sh in PATH")
}
