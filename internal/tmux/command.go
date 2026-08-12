package tmux

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/andyrewlee/amux/internal/shellutil"
)

// ClientCommandParams holds the parameters for building a tmux client command.
type ClientCommandParams struct {
	WorkDir        string
	Command        string
	Environment    []string
	Options        Options
	Tags           SessionTags
	DetachExisting bool // Detach other clients attached to this session.
}

// NewClientCommand builds the shell command string that creates (or reattaches to)
// a tmux session with the given name and parameters.
func NewClientCommand(sessionName string, p ClientCommandParams) string {
	if p.Options == (Options{}) {
		p.Options = DefaultOptions()
	}
	return clientCommand(sessionName, p.WorkDir, p.Command, p.Environment, p.Options, p.Tags, p.DetachExisting)
}

func clientCommand(sessionName, workDir, command string, environment []string, opts Options, tags SessionTags, detachExisting bool) string {
	// The tmux client process runs outside workDir so the shared server cannot
	// inherit a deletable workspace cwd. Preserve the old behavior for relative
	// config paths by resolving them against the workspace before constructing
	// the command.
	if opts.ConfigPath != "" && !filepath.IsAbs(opts.ConfigPath) {
		opts.ConfigPath = filepath.Join(workDir, opts.ConfigPath)
	}
	base := tmuxBase(opts)
	session := shellutil.ShellQuote(sessionName)
	optionTgt := shellutil.ShellQuote(exactSessionOptionTarget(sessionName))
	sessionTgt := shellutil.ShellQuote(sessionTarget(sessionName))
	dir := shellutil.ShellQuote(workDir)
	// Strip tmux-specific vars inside managed panes so `tmux` commands do not
	// accidentally target the AMUX control server.
	command = "unset TMUX TMUX_PANE; " + command
	cmd := shellutil.ShellQuote(command)
	paneEnvironment := make([]string, 0, len(environment))
	for _, assignment := range environment {
		if assignment != "" {
			paneEnvironment = append(paneEnvironment, shellutil.ShellQuote(assignment))
		}
	}
	paneEnvironmentArgs := ""
	if len(paneEnvironment) > 0 {
		paneEnvironmentArgs = " " + strings.Join(paneEnvironment, " ")
	}
	// The trampoline's $0 is a fixed label, $1 is workDir, and the remaining
	// arguments are the environment assignments plus the final shell argv.
	// Keeping values in positional parameters avoids evaluating any workspace
	// path or environment value as shell source.
	chdirScript := shellutil.ShellQuote(`cd "$1" && shift && exec env "$@"`)
	paneCommand := fmt.Sprintf("sh -c %s amux-chdir %s%s sh -lc %s", chdirScript, dir, paneEnvironmentArgs, cmd)

	// Ensure the session/server exists without attaching yet. tmux computes
	// client features at attach time, so the server option below must be set
	// while the server is alive but before the final attach command.
	// tmux keeps the server's original cwd open for its lifetime. If amux was
	// launched from a managed workspace and that workspace is later deleted,
	// tmux can leave a new pane in that deleted directory even when new-session
	// receives an absolute -c path (observed on tmux 3.7b). Run the pane command
	// through a POSIX-shell trampoline that performs a second, process-level
	// chdir, so both fresh and already poisoned servers start the final shell in
	// workDir. This intentionally avoids env -C, which is absent on macOS 14 and
	// BusyBox-based Linux systems.
	ensureSession := fmt.Sprintf("(%s has-session -t %s 2>/dev/null || %s new-session -ds %s -c %s %s || %s has-session -t %s 2>/dev/null)",
		base, sessionTgt, base, session, dir, paneCommand, base, sessionTgt)

	// Advertise DEC 2026 synchronized-output support before attaching. The
	// indexed slot keeps repeated session creates idempotent on amux's
	// dedicated server; the pattern matches the TERM amux sets for attach PTYs.
	// Swallowed on tmux < 3.2 (no terminal-features).
	syncFeatureSet := "(" + base + " set-option -s 'terminal-features[16]' 'xterm*:sync' 2>/dev/null || true)"

	settings := sessionSettingArgs(optionTgt, opts, tags)

	// Attach to the session, optionally detaching other clients.
	attachFlag := "-t"
	if detachExisting {
		attachFlag = "-dt"
	}
	attach := fmt.Sprintf("%s attach %s %s", base, attachFlag, sessionTgt)

	// The settings are braced so their internal `||` fallback cannot bind to the
	// preceding `&&`. Without the braces, sh's equal-precedence left-associative
	// parsing makes a failed ensureSession fall through into the per-option
	// fallback, spending a tmux round-trip per option against a session that does
	// not exist.
	return fmt.Sprintf("%s && %s && { %s}; %s", ensureSession, syncFeatureSet, settingsScript(base, settings), attach)
}

// sessionSettingArgs returns the argument list of every `set-option` amux applies
// to a managed session, in apply order. Each entry is the argv that follows
// `set-option`, so the same list can be rendered either chained into one tmux
// invocation or as separate ones.
func sessionSettingArgs(optionTgt string, opts Options, tags SessionTags) [][]string {
	// Disable tmux prefix for this session only (not global) to make it transparent
	settings := [][]string{
		{"-t", optionTgt, "prefix", "None"},
		{"-t", optionTgt, "prefix2", "None"},
	}
	if opts.HideStatus {
		settings = append(settings, []string{"-t", optionTgt, "status", "off"})
	}
	if opts.DisableMouse {
		settings = append(settings, []string{"-t", optionTgt, "mouse", "off"})
	}
	if opts.DefaultTerminal != "" {
		settings = append(settings, []string{"-t", optionTgt, "default-terminal", shellutil.ShellQuote(opts.DefaultTerminal)})
	}
	// Ensure activity timestamps update for window_activity-based tracking.
	settings = append(settings, []string{"-t", optionTgt, "-w", "monitor-activity", "on"})
	return append(settings, sessionTagArgs(optionTgt, tags)...)
}

// settingsScript renders the session settings as shell text, always terminated
// with "; " so the attach that follows runs regardless of the outcome.
//
// The happy path chains every set-option into a single tmux invocation using
// tmux's own `;` command separator. That matters because amux runs one shared,
// single-threaded tmux server: on a busy server each exec is a round-trip
// costing tens to hundreds of milliseconds, and a reattach used to pay for ~20
// of them here alone.
//
// tmux aborts a chained sequence at the first command that fails, so a single
// option that a given tmux version rejects would skip every option after it. The
// `||` fallback re-runs the same options one invocation at a time, each with its
// own stderr suppressed, which is exactly the per-option best-effort behavior
// this used to have. set-option is idempotent, so re-applying the ones that
// already succeeded is harmless.
func settingsScript(base string, settings [][]string) string {
	if len(settings) == 0 {
		return ""
	}

	var chained strings.Builder
	chained.WriteString(base)
	for i, args := range settings {
		if i > 0 {
			chained.WriteString(" ';'")
		}
		chained.WriteString(" set-option ")
		chained.WriteString(strings.Join(args, " "))
	}

	var sequential strings.Builder
	for _, args := range settings {
		sequential.WriteString(fmt.Sprintf("%s set-option %s 2>/dev/null; ", base, strings.Join(args, " ")))
	}

	return fmt.Sprintf("{ %s; } 2>/dev/null || { %strue; }; ", chained.String(), sequential.String())
}

func sessionTagArgs(session string, tags SessionTags) [][]string {
	if tags.WorkspaceID == "" && tags.TabID == "" && tags.Type == "" && tags.Assistant == "" && tags.CreatedAt == 0 && tags.InstanceID == "" && tags.SessionOwner == "" && tags.LeaseAtMS == 0 {
		return nil
	}
	args := [][]string{{"-t", session, "@amux", "1"}}
	entries := []struct{ key, value string }{
		{"@amux_workspace", tags.WorkspaceID},
		{"@amux_tab", tags.TabID},
		{"@amux_type", tags.Type},
		{"@amux_assistant", tags.Assistant},
		{"@amux_created_at", formatInt64NonZero(tags.CreatedAt)},
		{"@amux_instance", tags.InstanceID},
		{TagSessionOwner, tags.SessionOwner},
		{TagSessionLeaseAt, formatInt64Positive(tags.LeaseAtMS)},
		{TagSessionOwnerHeartbeatAt, formatInt64Positive(tags.LeaseAtMS)},
	}
	for _, e := range entries {
		if e.value != "" {
			args = append(args, []string{"-t", session, e.key, shellutil.ShellQuote(e.value)})
		}
	}
	return args
}

func formatInt64NonZero(v int64) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatInt(v, 10)
}

func formatInt64Positive(v int64) string {
	if v <= 0 {
		return ""
	}
	return strconv.FormatInt(v, 10)
}
