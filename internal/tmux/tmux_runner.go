package tmux

import "os/exec"

// runTmuxCmd executes a tmux command and returns its stdout. It is the single
// exec choke point for read paths that only need stdout (listTmux, runTmux,
// SessionTagValue, SessionCreatedAt, listSessionsWithTags). It is a package var
// so a test can swap it for a fake that returns canned ([]byte, error) — in
// particular an *exec.ExitError with code 1 — and drive the
// exit-1-means-empty / isExitCode1-but-other-error branches without a live tmux
// server. Mirrors the enterSleep var-seam precedent in send.go.
var runTmuxCmd = func(cmd *exec.Cmd) ([]byte, error) { return cmd.Output() }

// runTmuxCmdCombined executes a tmux command and returns its combined
// stdout+stderr. It is the choke point for sites that classify tmux stderr
// (SessionNamesWithClients, SetSessionTagValues, the global-option setters):
// stderr text decides whether an exit-code-1 failure is "treat as empty" or a
// real error, so the combined output must be captured. Swappable in tests for
// the same reason as runTmuxCmd.
var runTmuxCmdCombined = func(cmd *exec.Cmd) ([]byte, error) { return cmd.CombinedOutput() }
