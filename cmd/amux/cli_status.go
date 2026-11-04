package main

// The Program Status Protocol CLI surface: `amux report` (programs self-
// report from inside a pane), `amux wait` (agents and scripts block on a
// reported state), and the hidden `--internal-status-tap` helper that the
// pipe-pane install runs per hosted pane to harvest OSC 7501 sequences from
// the raw stream. Everything here runs without a TTY and must never
// initialize the TUI, logging, or config — a pane has no terminal on stdin,
// exactly like the panelaunch helper.

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/agentstatus"
	"github.com/andyrewlee/amux/internal/tmux"
)

const statusTapFlag = tmux.StatusTapInvocation

// HandleStatusCLI dispatches the status subcommands. It returns handled=false
// for any argv it does not own; a reserved tap invocation with malformed
// arguments is a handled failure (exit 2), so a stray helper argv can never
// fall through into TUI startup.
func HandleStatusCLI(args []string) (handled bool, exitCode int) {
	if len(args) == 0 {
		return false, 0
	}
	if args[0] == statusTapFlag {
		return tmux.HandleStatusTapInvocation(args, os.Stdin)
	}
	switch args[0] {
	case "report":
		return true, runReportCommand(args[1:])
	case "wait":
		return true, runWaitCommand(args[1:])
	default:
		return false, 0
	}
}

// runReportCommand implements `amux report`: print one OSC 7501 Program
// Status Protocol sequence on stdout. The pane's output stream IS the
// protocol's transport — amux installs a pipe-pane tap on every hosted
// pane that captures the sequence — so reporting needs no socket, no
// target flags, and works identically when the terminal itself grows
// protocol support. Well-behaved terminals ignore unknown OSCs, so the
// print is harmless everywhere else.
func runReportCommand(args []string) int {
	fs := flag.NewFlagSet("amux report", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	state := fs.String("state", "", "state to report (idle, working, done, blocked, error)")
	kind := fs.String("kind", "", "with blocked: permission, question, or auth")
	app := fs.String("app", "", "reporter identity (e.g. claude-code, run-script)")
	message := fs.String("message", "", "one-line context, e.g. what decision is needed")
	progress := fs.Int("progress", -1, "with working/blocked: 0-100, or -1 for none")
	clearFlag := fs.Bool("clear", false, "remove every reported status for this program")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "amux report: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *clearFlag {
		*state = agentstatus.StateClear
	}
	if *state == "" {
		fmt.Fprintln(os.Stderr, "amux report: --state is required (or pass --clear)")
		return 2
	}
	report := agentstatus.Report{
		State:    *state,
		Kind:     *kind,
		App:      *app,
		Message:  *message,
		Progress: *progress,
	}
	// Validate exactly like a spec consumer would: encode, parse, and
	// require the same report back. One comparison catches everything —
	// unknown state, malformed id, message cap or control runes, and the
	// flags whose values consumers treat as absent (kind/app grammar,
	// progress range) — so the CLI never emits a sequence that would be
	// silently dropped or half-read downstream. It also guarantees no
	// flag value can terminate the OSC early: control bytes fall outside
	// the value grammar, so the re-parse drops the pair and the round
	// trip diverges.
	parsed, ok := agentstatus.ParsePayload(report.EncodePayload())
	if !ok || parsed != report {
		fmt.Fprintf(os.Stderr, "amux report: invalid report (check --state/--kind/--app/--progress/--message values)\n")
		return 2
	}
	fmt.Printf("\x1b]7501;%s\x07", report.EncodePayload())
	return 0
}

// runWaitCommand implements `amux wait`: block until some pane reports a
// matching state. Exit 0 on a match; 1 when tmux itself is unusable (a
// permanent failure no amount of waiting fixes); 2 on timeout or a usage
// error — one code for "did not match", so scripts branch on exactly
// three outcomes. Transient read errors surface on stderr but keep
// waiting (the session may still be starting). The default state set is
// blocked,error,done — the states a supervising agent waits on.
func runWaitCommand(args []string) int {
	fs := flag.NewFlagSet("amux wait", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	states := fs.String("state", "blocked,error,done", "comma-separated reported states to wait for (idle, working, done, blocked, error, or 'any')")
	workspace := fs.String("workspace", "", "only wait for panes of this workspace name")
	session := fs.String("session", "", "only wait for this exact tmux session name")
	timeout := fs.Duration("timeout", 0, "give up after this long (0 = wait forever)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "amux wait: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *workspace != "" && *session != "" {
		fmt.Fprintln(os.Stderr, "amux wait: --workspace and --session are mutually exclusive")
		return 2
	}
	want, err := parseWaitStates(*states)
	if err != nil {
		fmt.Fprintf(os.Stderr, "amux wait: %v\n", err)
		return 2
	}
	opts := tmux.OptionsFromTMUXEnv()
	var deadline time.Time
	if *timeout > 0 {
		deadline = time.Now().Add(*timeout)
	}
	for {
		matched, err := findReportMatch(opts, want, *workspace, *session)
		switch {
		case err == nil:
			printReportMatch(matched)
			return 0
		case errors.Is(err, errNoMatch):
			// nothing matched yet — fall through to the deadline
		case tmux.IsUnavailableError(err):
			fmt.Fprintf(os.Stderr, "amux wait: %v\n", err)
			return 1
		case tmux.IsNoServerError(err):
			// a missing server simply has nothing to report yet
		default:
			fmt.Fprintf(os.Stderr, "amux wait: %v\n", err)
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr, "amux wait: timed out")
			return 2
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// parseWaitStates parses the --state filter: a comma-separated list of
// protocol states plus the 'any' wildcard. Unknown labels are refused up
// front — a typo would otherwise produce a filter nothing can ever match
// and a silent wait forever.
func parseWaitStates(raw string) (map[string]bool, error) {
	out := make(map[string]bool)
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		switch s {
		case "any",
			agentstatus.StateIdle, agentstatus.StateWorking, agentstatus.StateDone,
			agentstatus.StateBlocked, agentstatus.StateError:
			out[s] = true
		default:
			return nil, fmt.Errorf("unknown state %q (valid: idle, working, done, blocked, error, any)", s)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no states to wait for")
	}
	return out, nil
}

// waitMatch is one matched report with its addressing, for printing.
type waitMatch struct {
	Session string
	Report  agentstatus.Report
}

// errNoMatch is findReportMatch's not-yet signal: the scan succeeded but
// nothing matched. It is distinguishable from real scan errors so the
// caller can keep waiting without printing.
var errNoMatch = errors.New("no matching report yet")

// findReportMatch reads every pane report and returns the first match
// under the filters, deterministically ordered by session name, or
// errNoMatch. Workspace filtering joins the display tag
// @amux_workspace_name (and the id tag @amux_workspace) the same way the
// activity scan does.
func findReportMatch(opts tmux.Options, want map[string]bool, workspace, session string) (waitMatch, error) {
	reports, err := tmux.PaneReports(opts)
	if err != nil {
		return waitMatch{}, err
	}
	names := make([]string, 0, len(reports))
	for name := range reports {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if session != "" && name != session {
			continue
		}
		r := reports[name]
		// The protocol's process-exit lifetime: working/blocked are
		// transient and do not survive the pane's process. A stored
		// transient report from a dead pane is stale, not a match — the
		// pane's program can no longer act on whatever waits for it.
		if r.PaneDead && (r.State == agentstatus.StateWorking || r.State == agentstatus.StateBlocked) {
			continue
		}
		if !want["any"] && !want[r.State] {
			continue
		}
		if workspace != "" && !sessionWorkspaceMatches(opts, name, workspace) {
			continue
		}
		return waitMatch{Session: name, Report: r}, nil
	}
	return waitMatch{}, errNoMatch
}

// sessionWorkspaceMatches resolves a session's workspace through its
// @amux_workspace_name / @amux_workspace tags, matching the user-facing
// workspace name (the id tag is the fallback for tag-less comparisons).
func sessionWorkspaceMatches(opts tmux.Options, sessionName, workspace string) bool {
	rows, err := tmux.SessionsWithTags(map[string]string{}, []string{"@amux_workspace_name", "@amux_workspace"}, opts)
	if err != nil {
		return false
	}
	for _, row := range rows {
		if row.Name != sessionName {
			continue
		}
		return row.Tags["@amux_workspace_name"] == workspace || row.Tags["@amux_workspace"] == workspace
	}
	return false
}

func printReportMatch(m waitMatch) {
	parts := []string{
		"session=" + m.Session,
		"state=" + m.Report.State,
	}
	if m.Report.Kind != "" {
		parts = append(parts, "kind="+m.Report.Kind)
	}
	if m.Report.App != "" {
		parts = append(parts, "app="+m.Report.App)
	}
	if m.Report.Message != "" {
		parts = append(parts, "message="+m.Report.Message)
	}
	fmt.Println(strings.Join(parts, " "))
}
