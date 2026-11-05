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

// runWaitCommand implements `amux wait`: block until a pane reports a
// matching state, or until a pane under the filters exits while we are
// watching. The exit ladder is the machine contract:
//
//   - 0 — a pane reported a matching state (printed as session=… state=…);
//     also the passthrough exit of a program that exited 0.
//   - N — a pane's process exited while we watched; N is that program's
//     real exit status (printed as session=… exit=N, with the surviving
//     report's fields when one exists). tmux records no numeric status
//     for processes killed by a signal — those fall to the 1 rung as
//     exit=unknown.
//   - 4 — tmux itself is unusable (missing or too old — waiting cannot
//     fix that).
//   - 2 — timeout or a usage error.
//   - 1 — an observed exit whose status tmux did not record.
//
// Scan errors that are not unavailability are transient by contract:
// they surface on stderr and wait keeps going, ending at the timeout's 2.
//
// Exit observation begins when wait begins: sessions already dead at the
// first successful scan never fire (a lingering dead run session is
// history, not news), so an orchestrator starts wait before spawning
// what it waits on. Only sessions that keep their dead pane can pass an
// exit through — run sessions do (remain-on-exit), agent tab panes are
// destroyed with their process, so a tab's exit is invisible to wait and
// its surviving done/error reports remain the signal. An observed exit
// fires regardless of --state: a dead process can report nothing more,
// so waiting on it is over. The default state set is
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
	w := &waitWatcher{
		opts:      tmux.OptionsFromTMUXEnv(),
		want:      want,
		workspace: *workspace,
		session:   *session,
	}
	var deadline time.Time
	if *timeout > 0 {
		deadline = time.Now().Add(*timeout)
	}
	for {
		code, done := w.poll()
		if done {
			return code
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr, "amux wait: timed out")
			return 2
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// waitWatcher is one `amux wait` invocation's scan state: the filters,
// the report state set, and the baseline of sessions already dead when
// observation started (which never fire — see runWaitCommand).
type waitWatcher struct {
	opts      tmux.Options
	want      map[string]bool
	workspace string
	session   string
	baseline  map[string]bool
}

// poll runs one scan. It returns done=true with the exit code to exit
// with (0 for a state match, the observed program's status for an exit
// event), or done=false to keep waiting, with transient errors already
// printed to stderr.
func (w *waitWatcher) poll() (int, bool) {
	reports, rerr := tmux.PaneReports(w.opts)
	// Liveness comes from the same batched scan the dashboard uses
	// (AllSessionStates: dead == exists && !hasLivePane) — report options
	// only exist for panes that reported, and exit observation must not
	// depend on the program having spoken the protocol.
	states, serr := tmux.AllSessionStates(w.opts)
	for _, err := range []error{rerr, serr} {
		if err == nil {
			continue
		}
		switch {
		case tmux.IsUnavailableError(err):
			// a permanent failure no amount of waiting fixes
			fmt.Fprintf(os.Stderr, "amux wait: %v\n", err)
			return 4, true
		case tmux.IsNoServerError(err):
			// a missing server simply has nothing to report yet
		default:
			fmt.Fprintf(os.Stderr, "amux wait: %v\n", err)
		}
	}
	if reports == nil {
		reports = map[string]agentstatus.Report{}
	}
	u := w.universe(reports)
	dead := deadSessions(u, states)
	if w.baseline == nil && serr == nil {
		w.baseline = dead
	}
	// An observed process exit outranks a state match: the process is
	// over, no further reports can arrive, and its status is the final
	// answer.
	if m, ok := w.findExitEvent(u, reports, dead); ok {
		if code, ok := w.resolveExit(m); ok {
			return code, true
		}
		// The event evaporated between scans (session or pane gone) —
		// not an observable exit. Fall through and keep waiting.
	}
	if m, err := w.findMatch(reports); err == nil {
		printReportMatch(m)
		return 0, true
	}
	return 0, false
}

// universe is the set of session names exit observation applies to: the
// exact --session, the workspace's hosted sessions, or (unfiltered)
// every amux-hosted session plus every pane carrying a stored report.
// Workspace filtering joins through matchesWorkspace — the single
// workspace-join predicate the agent verbs share — so `wait
// --workspace` cannot disagree with `agent ls`/`send` about what a
// selector matches.
func (w *waitWatcher) universe(reports map[string]agentstatus.Report) map[string]bool {
	if w.session != "" {
		return map[string]bool{w.session: true}
	}
	rows, err := tmux.SessionsWithTags(nil, []string{"@amux_workspace", "@amux_workspace_name", "@amux_type"}, w.opts)
	if err != nil {
		rows = nil // no server or transient: hosted universe is empty this poll
	}
	out := make(map[string]bool, len(rows)+len(reports))
	for _, row := range rows {
		id, typ := row.Tags["@amux_workspace"], row.Tags["@amux_type"]
		if id == "" && typ == "" {
			continue // not amux-hosted
		}
		if w.workspace != "" {
			name := row.Tags["@amux_workspace_name"]
			if name == "" {
				name = id
			}
			if !(agentListRow{Workspace: name, WorkspaceID: id}).matchesWorkspace(w.workspace) {
				continue
			}
		}
		out[row.Name] = true
	}
	if w.workspace == "" {
		for name := range reports {
			out[name] = true
		}
	}
	return out
}

// deadSessions is the per-poll dead set — universe sessions whose every
// pane's process has exited (AllSessionStates' fold; a nil states map
// from a failed scan yields none).
func deadSessions(u map[string]bool, states map[string]tmux.SessionState) map[string]bool {
	dead := make(map[string]bool, len(states))
	for name := range u {
		if st, ok := states[name]; ok && st.Exists && !st.HasLivePane {
			dead[name] = true
		}
	}
	return dead
}

// findExitEvent returns the deterministically-first session in the
// universe whose process exited since the baseline (a session already
// dead when observation started never fires), with the pane's surviving
// report attached when one exists. The dead set is authoritative for the
// pane's lifetime: a report fetched moments before the death still says
// the pane was alive, so its transient flag is forced before the
// process-exit lifetime is applied — a working/blocked state must not
// outlive a death observed in the same poll.
func (w *waitWatcher) findExitEvent(u map[string]bool, reports map[string]agentstatus.Report, dead map[string]bool) (waitMatch, bool) {
	pick := ""
	for name := range dead {
		if w.baseline[name] || !u[name] {
			continue
		}
		if pick == "" || name < pick {
			pick = name
		}
	}
	if pick == "" {
		return waitMatch{}, false
	}
	m := waitMatch{Session: pick}
	if r, ok := reports[pick]; ok {
		r.PaneDead = true
		if r.SurvivesProcessExit() {
			m.Report = r
		}
	}
	return m, true
}

// resolveExit recovers the program's exit status for an observed exit
// and prints the match line. ok=false means the exit is not
// passthrough-able (the session or its pane vanished between scans, or
// the status probe failed transiently) and waiting should continue.
func (w *waitWatcher) resolveExit(m waitMatch) (int, bool) {
	exists, alive, code, err := tmux.RunSessionStatus(m.Session, w.opts)
	if err != nil {
		// Transient probe failure: the next poll re-fires the event.
		if !tmux.IsNoServerError(err) && !tmux.IsUnavailableError(err) {
			fmt.Fprintf(os.Stderr, "amux wait: %v\n", err)
		}
		return 0, false
	}
	if !exists || alive {
		// A destroyed pane carries no status (an agent tab's exit is
		// invisible by design); a live pane has not exited.
		return 0, false
	}
	if code < 0 {
		// Dead, but this tmux recorded no numeric status: a server older
		// than 3.3 without a parsable banner, or a process killed by a
		// signal (tmux names the signal instead of a number). The exit
		// is real; the status is not recoverable.
		printExitMatch(m, -1)
		fmt.Fprintln(os.Stderr, "amux wait: exit status not recorded by tmux")
		return 1, true
	}
	printExitMatch(m, code&0xFF)
	return code & 0xFF, true
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

// errNoMatch is findMatch's not-yet signal: the scan succeeded but
// nothing matched. It is distinguishable from real scan errors so the
// caller can keep waiting without printing.
var errNoMatch = errors.New("no matching report yet")

// findMatch reads the already-fetched pane reports and returns the first
// match under the filters, deterministically ordered by session name, or
// errNoMatch. Workspace filtering joins the display tag
// @amux_workspace_name (and the id tag @amux_workspace) the same way the
// activity scan does.
func (w *waitWatcher) findMatch(reports map[string]agentstatus.Report) (waitMatch, error) {
	names := make([]string, 0, len(reports))
	for name := range reports {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if w.session != "" && name != w.session {
			continue
		}
		r := reports[name]
		// The protocol's process-exit lifetime: working/blocked are
		// transient and do not survive the pane's process. A stored
		// transient report from a dead pane is stale, not a match — the
		// pane's program can no longer act on whatever waits for it.
		if !r.SurvivesProcessExit() {
			continue
		}
		if !w.want["any"] && !w.want[r.State] {
			continue
		}
		if w.workspace != "" && !sessionWorkspaceMatches(w.opts, name, w.workspace) {
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
	parts := appendReportParts([]string{"session=" + m.Session}, m.Report)
	fmt.Println(strings.Join(parts, " "))
}

// printExitMatch prints an observed process exit: the session, the
// surviving report's fields when one exists (done/error survive the
// process; transient states do not — an absent report is an empty
// State), and the program's exit status — exit=unknown when tmux
// recorded none.
func printExitMatch(m waitMatch, status int) {
	parts := []string{"session=" + m.Session}
	if m.Report.State != "" {
		parts = appendReportParts(parts, m.Report)
	}
	if status >= 0 {
		parts = append(parts, fmt.Sprintf("exit=%d", status))
	} else {
		parts = append(parts, "exit=unknown")
	}
	fmt.Println(strings.Join(parts, " "))
}

func appendReportParts(parts []string, r agentstatus.Report) []string {
	parts = append(parts, "state="+r.State)
	if r.Kind != "" {
		parts = append(parts, "kind="+r.Kind)
	}
	if r.App != "" {
		parts = append(parts, "app="+r.App)
	}
	if r.Message != "" {
		parts = append(parts, "message="+r.Message)
	}
	return parts
}
