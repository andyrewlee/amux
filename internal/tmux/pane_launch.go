package tmux

import (
	"fmt"
	"os"
	"strings"

	"github.com/andyrewlee/amux/internal/panelaunch"
	"github.com/andyrewlee/amux/internal/shellutil"
)

// PreparedCommand is a rendered tmux client command plus the owned private
// launch payload its pane invocation references. Command is the only safe
// string to execute; the payload carries the environment by file instead of
// argv. Ownership rules:
//
//   - If the caller never starts the child (or start fails before exec is
//     possible), it must AbortBeforeStart to remove the unused attempt.
//   - Once a child may have started, do NOT eagerly discard: a tmux create
//     acknowledgement is not proof the pane opened the payload yet, and the
//     same ambiguity applies to create-race and timeout outcomes. The
//     generated script already discards provably-unused attempts in-band;
//     anything else expires via the five-minute budget and the janitor.
type PreparedCommand struct {
	Command string
	payload *panelaunch.Prepared
}

// AbortBeforeStart removes this attempt's unused payload. Call it only when
// no pane process can have started — a successful PTY spawn or a dispatched
// tmux command leaves consumption ambiguous and must let the payload be
// consumed or expire instead.
func (p *PreparedCommand) AbortBeforeStart() {
	_ = p.payload.Discard()
}

// PayloadPath exposes the attempt's payload path for lifecycle assertions.
func (p *PreparedCommand) PayloadPath() string { return p.payload.Path() }

// preparedLaunch bundles one attempt's rendered shell fragments and its
// owned payload so ensureSessionScript can wire the lifecycle branches.
type preparedLaunch struct {
	runArgv     string // '<exe>' --internal-pane-launch run '<payload>'
	discardArgv string // '<exe>' --internal-pane-launch discard '<payload>'
	payload     *panelaunch.Prepared
}

// prepareLaunch writes the private payload and renders the quoted helper
// argv for both pane consumption and unused-attempt discard. The launcher is
// this executable: the same binary that renders the command is guaranteed
// present at pane spawn time, and test binaries dispatch the helper through
// their TestMain.
func prepareLaunch(workDir, command string, environment []string) (*preparedLaunch, error) {
	// The consumer merges over the pane's inherited environment and replaces
	// itself with `sh -lc <command>`; keep the historical tmux-var strip at
	// the head of the command, as the old `env` delivery did.
	payload, err := panelaunch.Prepare(workDir, "unset TMUX TMUX_PANE; "+command, environment)
	if err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		_ = payload.Discard()
		return nil, fmt.Errorf("pane launch: resolve launcher: %w", err)
	}
	return &preparedLaunch{
		runArgv:     quoteArgv(panelaunch.InvocationArgv(exe, "run", payload.Path())),
		discardArgv: quoteArgv(panelaunch.InvocationArgv(exe, "discard", payload.Path())),
		payload:     payload,
	}, nil
}

func quoteArgv(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellutil.ShellQuote(a)
	}
	return strings.Join(quoted, " ")
}

// ensureSessionScript renders the "create unless present" shell fragment:
// has-session, else new-session -ds running the pane invocation, else a
// final has-session that closes the create race between two concurrent
// creators. The fragment also carries the launch attempt's ownership rules:
//
//   - Already-present session: this attempt's payload is provably unused,
//     so the first branch discards it in-band (best effort — expiry is the
//     backstop) before settings/attach continue.
//   - Lost create race with an existing session: the winning pane's
//     recorded pane_start_command is the ownership proof. If it contains
//     this attempt's payload path our pane launched despite the reported
//     failure (e.g. a chained option failed after create) and the consumer
//     still needs the file — leave it. Any other winner means our pane
//     never ran, so the attempt is discarded. A failed query is ambiguous
//     and leaves the protected expiring payload.
//
// When keepDeadPane is true (detached run sessions), remain-on-exit is
// chained into the new-session invocation itself (';' commands run
// atomically on the tmux server): a fast-exiting pane can die and be reaped
// between the separate create and set-option round-trips, which would lose
// the session — and let a later Ensure respawn it — before the option ever
// lands. The option is idempotent, so the settings pass that follows still
// re-applies it for already-present sessions. Attach sessions pass
// keepDeadPane=false: their panes must not linger after the agent exits.
func ensureSessionScript(base, sessionName, dir string, launch *preparedLaunch, keepDeadPane bool) string {
	session := shellutil.ShellQuote(sessionName)
	sessionTgt := shellutil.ShellQuote(sessionTarget(sessionName))
	create := fmt.Sprintf("%s new-session -ds %s -c %s %s", base, session, dir, launch.runArgv)
	if keepDeadPane {
		// set-option's -t does not accept the '=' exact-match form (unlike
		// has-session's): use the bare session name, as sessionSettingArgs does.
		create += fmt.Sprintf(" ';' set-option -t %s remain-on-exit on", session)
	}
	present := fmt.Sprintf("(%s has-session -t %s 2>/dev/null && (%s || true))",
		base, sessionTgt, launch.discardArgv)
	race := fmt.Sprintf("(%s has-session -t %s 2>/dev/null && { "+
		"s=\"$(%s list-panes -t %s -F '#{pane_start_command}' 2>/dev/null)\"; "+
		"if [ -n \"$s\" ]; then case \"$s\" in *%s*) : ;; *) %s || true ;; esac; fi; })",
		base, sessionTgt, base, sessionTgt,
		shellutil.ShellQuote(launch.payload.Path()), launch.discardArgv)
	return fmt.Sprintf("%s || ( %s ) || %s", present, create, race)
}

// exitCodeCollision is the shell sentinel createSessionScript exits with on a
// confirmed name collision — distinct from every tmux error code so the Go
// caller can map it to ErrSessionNameTaken without parsing stderr.
const exitCodeCollision = 3

// createSessionScript is ensureSessionScript's allocation sibling: it
// reports a name collision instead of adopting the existing session. A
// same-named session present on entry — or a foreign winner surviving a
// failed create — exits exitCodeCollision after discarding this attempt's
// provably-unused payload in-band. The create-lost-to-our-own-pane case
// applies the settings post-hoc and succeeds, exactly like the ensure race
// branch; an empty pane_start_command query stays ambiguous (payload left to
// expire) and reports collision so the caller retries a fresh name.
//
// The settings text is threaded in because only the owner of a create should
// stamp tags: the collision branches must never touch a foreign session.
func createSessionScript(base, sessionName, dir string, launch *preparedLaunch, keepDeadPane bool, settings string) string {
	session := shellutil.ShellQuote(sessionName)
	sessionTgt := shellutil.ShellQuote(sessionTarget(sessionName))
	create := fmt.Sprintf("%s new-session -ds %s -c %s %s", base, session, dir, launch.runArgv)
	if keepDeadPane {
		create += fmt.Sprintf(" ';' set-option -t %s remain-on-exit on", session)
	}
	create = fmt.Sprintf("( %s ) && { %s}", create, settings)
	taken := fmt.Sprintf("(%s has-session -t %s 2>/dev/null && { %s || true; exit %d; })",
		base, sessionTgt, launch.discardArgv, exitCodeCollision)
	race := fmt.Sprintf("(%s has-session -t %s 2>/dev/null && { "+
		"s=\"$(%s list-panes -t %s -F '#{pane_start_command}' 2>/dev/null)\"; "+
		"if [ -n \"$s\" ]; then case \"$s\" in *%s*) { %s}; exit 0 ;; *) %s || true ;; esac; fi; exit %d; })",
		base, sessionTgt, base, sessionTgt,
		shellutil.ShellQuote(launch.payload.Path()), settings, launch.discardArgv, exitCodeCollision)
	return fmt.Sprintf("%s || %s || %s", taken, create, race)
}
