package main

// `amux send`: deliver input to exactly one hosted session. This is the
// write-side verb of the agent-facing surface — the counterpart of
// `amux wait` — and it encodes the input discipline docs/ORCHESTRATION.md
// pins for external orchestrators: exact-match targets, literal text, and
// a literal carriage return (never the named Enter key) submitted as a
// separate, delayed command.

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// sendTextSubmitDelay is the pause between literal text and a trailing
// carriage return: a CR fused onto text can arrive before a raw-mode
// agent is ready to read it, and the close-loop e2e suite pins the
// separated delivery. Mirrors amux's own send path.
const sendTextSubmitDelay = 150 * time.Millisecond

func runSendCommand(args []string) int {
	fs := flag.NewFlagSet("amux send", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	workspace := fs.String("workspace", "", "target the single session of this workspace name")
	session := fs.String("session", "", "target this exact session name")
	text := fs.String("text", "", "literal text to send (bytes verbatim, no submit)")
	enter := fs.Bool("enter", false, "with --text: submit with a literal carriage return after a short delay")
	keys := fs.String("keys", "", "comma-separated named keys (cr escape space tab up down left right home end pgup pgdn bspace delete insert f1..f12 c-x s-x m-x)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "amux send: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if (*workspace == "") == (*session == "") {
		fmt.Fprintln(os.Stderr, "amux send: exactly one of --workspace or --session is required")
		return 2
	}
	hasText, hasKeys := *text != "", *keys != ""
	if hasText == hasKeys {
		fmt.Fprintln(os.Stderr, "amux send: exactly one of --text or --keys is required")
		return 2
	}
	if *enter && hasKeys {
		fmt.Fprintln(os.Stderr, "amux send: --enter submits --text; for keys, send 'cr' in --keys")
		return 2
	}
	keyNames := parseSendKeys(*keys)
	if hasKeys && len(keyNames) == 0 {
		fmt.Fprintln(os.Stderr, "amux send: no keys in --keys")
		return 2
	}
	for _, key := range keyNames {
		if !tmux.ValidSendKey(key) {
			fmt.Fprintf(os.Stderr, "amux send: unknown key %q (see amux --skill for the key vocabulary)\n", key)
			return 2
		}
	}

	opts := tmux.OptionsFromTMUXEnv()
	target, err := resolveSendTarget(opts, *workspace, *session)
	if err != nil {
		fmt.Fprintf(os.Stderr, "amux send: %v\n", err)
		var se *sendExitError
		if errors.As(err, &se) {
			return se.code
		}
		return sendDeliveryExit(err)
	}

	if hasText {
		if err := tmux.SendSessionText(target, *text, opts); err != nil {
			fmt.Fprintf(os.Stderr, "amux send: %v\n", err)
			return sendDeliveryExit(err)
		}
		if *enter {
			time.Sleep(sendTextSubmitDelay)
			if err := tmux.SendSessionKeys(target, []string{"cr"}, opts); err != nil {
				fmt.Fprintf(os.Stderr, "amux send: %v\n", err)
				return sendDeliveryExit(err)
			}
		}
		return 0
	}
	if err := tmux.SendSessionKeys(target, keyNames, opts); err != nil {
		fmt.Fprintf(os.Stderr, "amux send: %v\n", err)
		return sendDeliveryExit(err)
	}
	return 0
}

// sendDeliveryExit maps a failure send could not resolve to a typed
// code: an unreachable server is 4 like every other reachability
// failure; the rest are generic operational failures (1).
func sendDeliveryExit(err error) int {
	if tmux.IsUnavailableError(err) {
		return 4
	}
	return 1
}

// sendExitError is a send failure a script can branch on by exit code:
// 3 = the target does not resolve to exactly one hosted session (not
// found or ambiguous — fix the invocation), 4 = unreachable — no tmux
// server to deliver through, or the target's pane process has exited so
// nothing can read the input (fix the environment or pick another
// target).
type sendExitError struct {
	code int
	msg  string
}

func (e *sendExitError) Error() string { return e.msg }

func sendErrorf(code int, format string, args ...any) error {
	return &sendExitError{code: code, msg: fmt.Sprintf(format, args...)}
}

func parseSendKeys(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// resolveSendTarget resolves the destination to exactly one LIVE hosted
// session. Ambiguity is an error that lists the candidates — guessing a
// target for injected input is the one place amux must never guess. Only
// amux-hosted sessions are addressable: a bare tmux session on the server
// is not amux's to type into. A session whose pane process has exited
// (remain-on-exit) is refused when its death is observable — a stored
// report annotated pane-dead — because send-keys would otherwise succeed
// against the dead pane with nothing to read it, reporting delivery that
// never happened. Failures are sendExitError-typed so `amux send` can
// report the stable exit codes: 3 for a selector that does not resolve
// to exactly one hosted session (not found, ambiguous), 4 for an
// unreachable destination (no tmux server, or the pane's process has
// exited).
func resolveSendTarget(opts tmux.Options, workspace, session string) (string, error) {
	rows, err := hostedAgentRows(opts)
	if err != nil {
		if tmux.IsUnavailableError(err) {
			return "", sendErrorf(4, "%v", err)
		}
		return "", err
	}
	if len(rows) == 0 && !tmux.HasServer(opts) {
		// The tag readers cannot distinguish "no server" from "server
		// with nothing hosted" (list-sessions' exit-1 swallowing), so
		// probe: an empty fleet is a selector failure, a missing
		// transport is unreachable.
		return "", sendErrorf(4, "no amux-hosted sessions (no tmux server running)")
	}
	matches := make([]string, 0, 2)
	for _, row := range rows {
		if session != "" {
			if row.Session == session {
				matches = append(matches, row.Session)
			}
			continue
		}
		if row.matchesWorkspace(workspace) {
			matches = append(matches, row.Session)
		}
	}
	switch len(matches) {
	case 1:
		for _, row := range rows {
			if row.Session == matches[0] && row.paneDead {
				return "", sendErrorf(4, "session %s: pane process has exited; nothing can read the input", matches[0])
			}
		}
		return matches[0], nil
	case 0:
		if session != "" {
			return "", sendErrorf(3, "no hosted session named %q", session)
		}
		return "", sendErrorf(3, "no hosted sessions for workspace %q", workspace)
	default:
		return "", sendErrorf(3, "%d sessions match; pass --session with one of: %s",
			len(matches), strings.Join(matches, ", "))
	}
}
