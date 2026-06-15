// Command fakeagent is a deterministic stand-in for a real raw-mode coding agent
// (Claude Code / Codex / Cline) for amux end-to-end tests.
//
// Unlike the inert "sleep 1000" stub, it puts its terminal into raw mode and
// records every byte it receives on stdin to the file named by $FAKEAGENT_LOG.
// Raw mode is the whole point: it disables the line discipline's CR->NL
// translation, so a literal carriage return (0x0D) survives intact instead of
// being mapped to NL. That makes amux's real input path observable and lets a
// test prove the historically-escaped send/Enter/timing bugs cannot regress:
//
//   - bug #2 named-"Enter" vs hex 0D: a regression to the named key never
//     reaches the agent as 0x0D, so the recording differs.
//   - bug #3 Enter lost when sent <50ms after text: the recording is missing
//     bytes or the trailing CR.
//   - bug #4 input sent before the agent is ready: the readiness banner gates
//     the test, and --startup-delay simulates a slow agent.
//
// It prints "FAKEAGENT READY" once raw mode is active so callers can wait for
// readiness before sending input, then echoes nothing (a real raw-mode TUI does
// not line-echo) and stays alive until stdin closes.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/term"
)

// readyBanner is emitted once the agent is in raw mode and ready for input.
// Tests wait for it before typing so input is never sent prematurely.
const readyBanner = "FAKEAGENT READY"

// syncer is the subset of *os.File the recorder needs to flush after each read.
// Extracting it lets recordStream accept an in-memory writer in tests while the
// real run passes the log file, whose Sync makes the appended bytes pollable.
type syncer interface {
	io.Writer
	Sync() error
}

// readyBannerBytes returns the exact bytes the agent emits once raw mode is
// active. \r\n (not \n) because the terminal is raw, so the kernel performs no
// NL->CRNL output translation and the banner must carry its own carriage return.
func readyBannerBytes() []byte {
	return []byte(readyBanner + "\r\n")
}

// recordStream is the load-bearing recorder: it copies every byte read from in
// to log, flushing (Sync) after each non-empty read so a polling test observes
// bytes deterministically, and returns when the reader reports an error.
//
// It must preserve bytes verbatim — in particular an embedded carriage return
// (0x0D) — because that is exactly what proves a real keystroke (hex 0D, not the
// named Enter key) reached a raw-mode agent. io.EOF is treated as a clean end of
// stream and reported as nil; any other read error is returned. A read that
// yields n>0 before signaling an error still records that partial buffer.
func recordStream(in io.Reader, log syncer) error {
	buf := make([]byte, 256)
	for {
		n, readErr := in.Read(buf)
		if n > 0 {
			if _, werr := log.Write(buf[:n]); werr == nil {
				_ = log.Sync() // flush per read so tests can poll deterministically
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return nil
			}
			return readErr
		}
	}
}

func main() {
	var startupDelay time.Duration
	flag.DurationVar(&startupDelay, "startup-delay", 0,
		"pause before signaling readiness, to simulate a slow-starting agent")
	flag.Parse()

	logPath := os.Getenv("FAKEAGENT_LOG")
	if logPath == "" {
		fmt.Fprintln(os.Stderr, "fakeagent: FAKEAGENT_LOG is not set")
		os.Exit(2)
	}

	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakeagent: open log:", err)
		os.Exit(2)
	}
	defer log.Close()

	// Put stdin into raw mode so received bytes are untranslated. Without this a
	// carriage return would be read as NL and the test could not tell hex 0D from
	// the named Enter key.
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		prev, err := term.MakeRaw(fd)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fakeagent: make raw:", err)
			os.Exit(2)
		}
		defer func() { _ = term.Restore(fd, prev) }()
	}

	if startupDelay > 0 {
		time.Sleep(startupDelay)
	}

	// Match full-screen terminal apps that ask their host to deliver wheel
	// events to stdin instead of using outer scrollback.
	fmt.Fprint(os.Stdout, "\x1b[?1000h\x1b[?1006h")
	os.Stdout.Write(readyBannerBytes())

	_ = recordStream(os.Stdin, log)
}
