package tmux

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

// These tests pin the attach-exclusivity MECHANISM against a real tmux
// server: a plain `attach -t` shares a session with the clients already
// viewing it, while `attach -dt` evicts them. amux renders only the plain
// form (NewClientCommand / AttachOnlyClientCommand never emit -d, pinned in
// command_test.go and tmux_test.go), so this file proves the tmux-side half
// of the no-detach-war contract: with the flags amux uses, a second client
// physically cannot kick the first.
//
// The detaching test is the red control — it deliberately runs one
// `attach -dt` and asserts the kick IS observed — so a green run proves the
// harness can detect kicks, which is what makes the "no kick" assertions
// non-vacuous.

// startAttachedClient starts a PTY-backed `tmux attach-session` client with
// the given extra attach flags and returns a stop function plus a recorder
// of everything the client printed, whose output carries tmux's own detach
// notice ("[detached (from session …)]") when the client is evicted. Unlike
// attachClient above it does not skip on attach failure: these tests assert
// on kick behavior, so a client that never attaches must fail the test, not
// skip it.
func startAttachedClient(t *testing.T, opts Options, session string, extraFlags ...string) (func(), *ptyRecorder) {
	t.Helper()

	args := tmuxArgs(opts, "attach-session")
	args = append(args, extraFlags...)
	args = append(args, "-t", session)
	cmd := exec.Command("tmux", args...)
	// A tmux client needs a usable TERM; without one the attach silently
	// never becomes a client (observed on macOS tmux 3.7b from a test
	// process whose environment carries no TERM).
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Skipf("cannot start PTY-backed tmux attach: %v", err)
	}
	rec := recordPTY(ptmx)
	stop := func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_ = ptmx.Close()
	}
	t.Cleanup(stop)
	return stop, rec
}

// ptyRecorder drains a client PTY continuously on a goroutine and
// accumulates everything it printed. Assertions then read a snapshot with
// output() and never block on terminal output: pty masters support no read
// deadlines on darwin (creack/pty builds the master without the runtime
// poller, so SetReadDeadline is a silent no-op and a direct Read blocks
// until the next write), which made deadline-capped polling both slow and
// hang-prone. Accumulating across reads also keeps a notice that straddles
// a read boundary detectable.
type ptyRecorder struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// recordPTY starts the drain goroutine for ptmx. It ends when the master is
// closed (or errors), which the caller's cleanup does.
func recordPTY(ptmx *os.File) *ptyRecorder {
	rec := &ptyRecorder{}
	go func() {
		chunk := make([]byte, 4096)
		for {
			n, err := ptmx.Read(chunk)
			if n > 0 {
				rec.mu.Lock()
				rec.buf.Write(chunk[:n])
				rec.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	return rec
}

// output returns everything the client has printed so far.
func (r *ptyRecorder) output() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

// waitForClientCount polls SessionClientCount until it equals want, failing
// (not skipping) on timeout — a client that never attaches here is a broken
// fixture, not an environmental limitation.
func waitForClientCount(t *testing.T, opts Options, session string, want int, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last int
	for time.Now().Before(deadline) {
		count, err := SessionClientCount(session, opts)
		if err == nil {
			if count == want {
				return
			}
			last = count
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("client count never reached %d while waiting for %s (last: %d)", want, what, last)
}

// TestPlainAttachSharesSessionWithExistingClient is the positive half of the
// mechanism: with the plain `attach -t` form amux renders, a second client
// attaches alongside the first and NOTHING evicts anyone for as long as both
// are connected. This is the multi-instance contract — two amux instances
// sharing a workspace both hold clients on the same agent session.
func TestPlainAttachSharesSessionWithExistingClient(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "share", "sleep 300")
	_, firstOut := startAttachedClient(t, opts, "share")
	waitForClientCount(t, opts, "share", 1, "first client")

	_, secondOut := startAttachedClient(t, opts, "share")
	waitForClientCount(t, opts, "share", 2, "second client")

	// Both clients must STILL be attached after a settle window — sharing is
	// steady state, not a race that resolves into a kick — and neither
	// terminal may show tmux's detach notice.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		count, err := SessionClientCount("share", opts)
		if err != nil {
			t.Fatalf("SessionClientCount: %v", err)
		}
		if count != 2 {
			t.Fatalf("plain attach evicted a client: count dropped to %d", count)
		}
		if out := firstOut.output(); strings.Contains(out, "[detached") {
			t.Fatalf("plain attach showed the evicted-client notice on the first client:\n%s", out)
		}
		if out := secondOut.output(); strings.Contains(out, "[detached") {
			t.Fatalf("plain attach showed the evicted-client notice on the second client:\n%s", out)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestDetachingAttachEvictsClients is the red control that validates the
// harness: `attach -dt` (the flag amux must never emit) kicks every other
// client — exactly the mechanism behind the multi-instance detach war and
// the "[detached (from session …)]" flash. If this test ever stops observing
// the kick, the no-kick assertions above have gone vacuous and must be
// fixed, not celebrated.
func TestDetachingAttachEvictsClients(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "evict", "sleep 300")
	_, victimOut := startAttachedClient(t, opts, "evict")
	waitForClientCount(t, opts, "evict", 1, "first client")

	startAttachedClient(t, opts, "evict", "-d")
	// The -d attach wins sole ownership: the first client is evicted.
	waitForClientCount(t, opts, "evict", 1, "eviction to take effect")

	// The evicted client's terminal carries the detach notice — the exact
	// text users saw flash before the war settled. The recorder accumulates
	// across reads, so a notice straddling a read boundary is still seen.
	var sawNotice bool
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !sawNotice {
		sawNotice = strings.Contains(victimOut.output(), "[detached")
		time.Sleep(50 * time.Millisecond)
	}
	if !sawNotice {
		t.Fatal("expected the evicted client's terminal to show tmux's [detached notice")
	}
}
