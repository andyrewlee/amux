package tmux

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/agentstatus"
	"github.com/andyrewlee/amux/internal/shellutil"
)

// Program Status Protocol tags. Reports arrive over the pane's output
// stream — either a spec-conformant OSC 7501 sequence from any program or
// the byte-identical `amux report` printf — captured by the pipe-pane tap,
// which maintains the protocol record set and writes the folded
// presentation view to these pane-scoped options, so every amux instance
// (and external orchestrators) reads the same live truth from the tmux
// server. Pane options die with their pane, which is the report's
// lifecycle: nothing outlives the hosted program.
const (
	TagReportedState   = "@amux_reported_state"
	TagReportedKind    = "@amux_reported_kind"
	TagReportedApp     = "@amux_reported_app"
	TagReportedMessage = "@amux_reported_message"
	TagReportedAt      = "@amux_reported_at"
)

// PaneReports reads every pane's report on the server in one list-panes
// call, keyed by session name. Only panes with a reported state appear.
// A multi-pane session folds to its highest-severity report; amux's hosted
// sessions are single-pane, so the fold only matters for hand-made panes.
func PaneReports(opts Options) (map[string]agentstatus.Report, error) {
	if err := EnsureAvailable(); err != nil {
		return nil, err
	}
	// Field order follows listSessionsWithTags: session_id leads, the fixed
	// report fields follow, session_name trails — a name containing '|' can
	// only extend the tail field, never shift the positional parse. Stored
	// values are sanitized against '|' at write time (sanitizeTagValue), so
	// the message field cannot split a row either. pane_dead rides along as
	// the consumer-side process-exit signal: the protocol drops working and
	// blocked records when the process exits, and a dead pane's pipe may
	// never deliver EOF to the tap, so the read side enforces it too.
	format := strings.Join([]string{
		"#{session_id}",
		"#{" + TagReportedState + "}",
		"#{" + TagReportedKind + "}",
		"#{" + TagReportedApp + "}",
		"#{" + TagReportedMessage + "}",
		"#{" + TagReportedAt + "}",
		"#{pane_dead}",
		"#{session_name}",
	}, tagFieldSeparator)
	cmd, cancel := tmuxCommand(opts, "list-panes", "-a", "-F", format)
	defer cancel()
	output, err := runTmuxCmd(cmd)
	if err != nil {
		if isExitCode1(err) {
			return nil, nil
		}
		return nil, err
	}
	return parsePaneReportRows(parseOutputLines(output)), nil
}

// parsePaneReportRows is the pure parse half of PaneReports. Malformed rows
// are dropped; a row without a state is not a report.
func parsePaneReportRows(lines []string) map[string]agentstatus.Report {
	out := make(map[string]agentstatus.Report)
	for _, line := range lines {
		parts := strings.SplitN(line, tagFieldSeparator, 8)
		if len(parts) != 8 || !isSessionIDToken(strings.TrimSpace(parts[0])) {
			continue
		}
		state := strings.TrimSpace(parts[1])
		if state == "" {
			continue
		}
		atMS, _ := strconv.ParseInt(strings.TrimSpace(parts[5]), 10, 64)
		r := agentstatus.Report{
			State:    state,
			Kind:     strings.TrimSpace(parts[2]),
			App:      strings.TrimSpace(parts[3]),
			Message:  strings.TrimSpace(parts[4]),
			AtMS:     atMS,
			PaneDead: strings.TrimSpace(parts[6]) == "1",
		}
		name := parts[7]
		if existing, ok := out[name]; ok {
			// Multi-pane session: the row needing attention most wins
			// WHOLESALE, its pane_dead flag included. Merging the flags
			// would break the process-exit lifetime: a live pane's
			// lower-severity report must not resurrect a dead pane's
			// blocked report (which the consumer must drop) by marking
			// the session alive.
			if agentstatus.SeverityOf(existing.State) >= agentstatus.SeverityOf(r.State) {
				continue
			}
		}
		out[name] = r
	}
	return out
}

// WritePaneReport stores the folded presentation view of one pane's
// records as pane options on target (a pane id such as %3, a session
// name, session id — anything tmux resolves to a pane). The tap is the
// only writer, and it serializes its writes, so no cross-writer guard is
// needed. Values are sanitized here so nothing a program reported can
// shift the batched read or inject tmux format syntax. An empty state
// clears every option — the record set is empty.
func WritePaneReport(target string, r agentstatus.Report, opts Options) error {
	if err := EnsureAvailable(); err != nil {
		return err
	}
	if r.State == "" {
		return ClearPaneReport(target, opts)
	}
	if r.AtMS == 0 {
		r.AtMS = time.Now().UnixMilli()
	}
	state := sanitizeTagValue(r.State)
	app := sanitizeTagValue(r.App)
	message := sanitizeTagValue(r.Message)
	kind := sanitizeTagValue(r.Kind)
	if state == "" {
		return errors.New("tmux: refusing to store a report without a state")
	}
	at := ""
	if r.AtMS != 0 {
		at = strconv.FormatInt(r.AtMS, 10)
	}
	// Absent fields are written as empty strings rather than unset: tmux
	// rejects `set-option -u` on a never-set user option, and one rejected
	// link would abort the whole ';' chain — losing the report. Empty and
	// unset read identically (PaneReports skips empty-state rows; empty
	// app/message parse to ""). The chain runs as one tmux command, so the
	// options land atomically for concurrent list-panes readers.
	chain := []string{"set-option", "-p", "-t", target, TagReportedState, state}
	chain = appendChainOption(chain, target, TagReportedKind, kind)
	chain = appendChainOption(chain, target, TagReportedApp, app)
	chain = appendChainOption(chain, target, TagReportedMessage, message)
	chain = appendChainOption(chain, target, TagReportedAt, at)
	cmd, cancel := tmuxCommand(opts, chain...)
	defer cancel()
	if _, err := runTmuxCmd(cmd); err != nil {
		return fmt.Errorf("tmux: write pane report: %w", err)
	}
	return nil
}

// ClearPaneReport unsets every report option on the pane — the release
// path for amux-initiated stops. After any report all options exist, so
// the -u chain completes; a pane that never reported has nothing to
// clear, and tmux's exit 1 there is success.
func ClearPaneReport(target string, opts Options) error {
	if err := EnsureAvailable(); err != nil {
		return err
	}
	args := []string{"set-option", "-p", "-t", target, "-u", TagReportedState}
	for _, key := range []string{TagReportedKind, TagReportedApp, TagReportedMessage, TagReportedAt} {
		args = append(args, ";", "set-option", "-p", "-t", target, "-u", key)
	}
	cmd, cancel := tmuxCommand(opts, args...)
	defer cancel()
	if _, err := runTmuxCmd(cmd); err != nil {
		if isExitCode1(err) {
			return nil
		}
		return fmt.Errorf("tmux: clear pane report: %w", err)
	}
	return nil
}

// appendChainOption appends one chained set-option to a tmux command argv,
// with the bare ";" argv separator in front — exec passes each element
// verbatim, so unlike the shell-text builders elsewhere in this package the
// separator must not be quoted. Empty values are written as empty strings
// (never -u) — see WritePaneReport.
func appendChainOption(chain []string, target, key, value string) []string {
	return append(chain, ";", "set-option", "-p", "-t", target, key, value)
}

// StatusTapCommand renders the pipe-pane installer for a hosted session:
// the amux binary in tap-helper mode receives the pane's raw output. -o
// keeps the install idempotent across reattach (no second pipe), and -O
// makes the pipe write-only so the helper's stdout can never leak into
// the pane. The tap command runs under the tmux server's shell, so every
// element is shell-quoted; the session target and helper argv are baked in
// at install time. The target is the bare session name: pipe-pane takes a
// pane target, and the '=name' exact-session form is rejected there
// ("can't find pane"), so a session-name target — which tmux resolves to
// the session's active pane at install, the hosted-session shape — is the
// correct address. pipe-pane format-expands its command, so '#{pane_id}'
// bakes in the very pane the pipe reads; the helper writes to that id,
// and a later split can never skew reports onto the session's new active
// pane. An empty binaryPath renders no install — the caller treats the
// tap as optional.
func StatusTapCommand(binaryPath, serverName, sessionName string, opts Options) string {
	if binaryPath == "" {
		return ""
	}
	base := tmuxBase(opts)
	sessionTgt := shellutil.ShellQuote(sessionName)
	helper := fmt.Sprintf("%s %s %s %s '#{pane_id}'",
		shellutil.ShellQuote(binaryPath),
		shellutil.ShellQuote(StatusTapInvocation),
		shellutil.ShellQuote(serverName),
		shellutil.ShellQuote(sessionName),
	)
	return fmt.Sprintf("%s pipe-pane -o -O -t %s %s", base, sessionTgt, shellutil.ShellQuote(helper))
}
