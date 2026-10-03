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

// NewClientCommand builds the shell command that creates (or reattaches to) a
// tmux session with the given name and parameters, returning it as an owned
// PreparedCommand. The pane's environment travels through a private launch
// payload rather than command arguments; the caller must consult
// PreparedCommand's ownership rules before discarding anything.
func NewClientCommand(sessionName string, p ClientCommandParams) (*PreparedCommand, error) {
	if p.Options == (Options{}) {
		p.Options = DefaultOptions()
	}
	opts := p.Options
	// The tmux client process runs outside workDir so the shared server cannot
	// inherit a deletable workspace cwd. Preserve the old behavior for relative
	// config paths by resolving them against the workspace before constructing
	// the command.
	if opts.ConfigPath != "" && !filepath.IsAbs(opts.ConfigPath) {
		opts.ConfigPath = filepath.Join(p.WorkDir, opts.ConfigPath)
	}
	base := tmuxBase(opts)
	optionTgt := shellutil.ShellQuote(exactSessionOptionTarget(sessionName))
	sessionTgt := shellutil.ShellQuote(sessionTarget(sessionName))
	dir := shellutil.ShellQuote(p.WorkDir)

	launch, err := prepareLaunch(p.WorkDir, p.Command, p.Environment)
	if err != nil {
		return nil, err
	}

	// Ensure the session/server exists without attaching yet. tmux computes
	// client features at attach time, so the server option below must be set
	// while the server is alive but before the final attach command.
	ensureSession := ensureSessionScript(base, sessionName, dir, launch, false)

	// Advertise DEC 2026 synchronized-output support before attaching. The
	// indexed slot keeps repeated session creates idempotent on amux's
	// dedicated server; the pattern matches the TERM amux sets for attach PTYs.
	// Swallowed on tmux < 3.2 (no terminal-features).
	syncFeatureSet := "(" + base + " set-option -s 'terminal-features[16]' 'xterm*:sync' 2>/dev/null || true)"

	settings := sessionSettingArgs(optionTgt, opts, p.Tags)

	// Attach to the session, optionally detaching other clients.
	attachFlag := "-t"
	if p.DetachExisting {
		attachFlag = "-dt"
	}
	attach := fmt.Sprintf("%s attach %s %s", base, attachFlag, sessionTgt)

	// The settings are braced so their internal `||` fallback cannot bind to the
	// preceding `&&`. Without the braces, sh's equal-precedence left-associative
	// parsing makes a failed ensureSession fall through into the per-option
	// fallback, spending a tmux round-trip per option against a session that does
	// not exist.
	return &PreparedCommand{
		Command: fmt.Sprintf("%s && %s && { %s}; %s", ensureSession, syncFeatureSet, settingsScript(base, settings), attach),
		payload: launch.payload,
	}, nil
}

// AttachOnlyClientCommand builds the shell command that attaches a tmux
// client to an EXISTING session — no ensure-session, no set-option, no tags.
// It exists for sessions the client does not own (run sessions): NewClientCommand
// would re-stamp amux's session settings and tab tags on attach, rewriting the
// session's identity out from under its real owner. If the session is gone the
// bare attach fails and the caller reports it — nothing is recreated.
func AttachOnlyClientCommand(sessionName string, opts Options) string {
	if opts == (Options{}) {
		opts = DefaultOptions()
	}
	base := tmuxBase(opts)
	sessionTgt := shellutil.ShellQuote(sessionTarget(sessionName))
	syncFeatureSet := "(" + base + " set-option -s 'terminal-features[16]' 'xterm*:sync' 2>/dev/null || true)"
	return fmt.Sprintf("%s; %s attach -dt %s", syncFeatureSet, base, sessionTgt)
}

// The pane's working directory and environment ride in the private launch
// payload prepared in pane_launch.go, never in argv. The payload consumer
// performs the process-level chdir itself: tmux keeps the server's original
// cwd open for its lifetime, so a deleted workspace cwd can poison a pane
// spawned with an absolute -c (observed on tmux 3.7b), and the native chdir
// is what guarantees both fresh and already poisoned servers start the final
// shell in workDir.

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

// SessionTagPair is one resolved @amux_* option/value pair.
type SessionTagPair struct {
	Key   string
	Value string
}

// SessionTagPairs resolves tags into the full ordered option/value set — the
// single mapping shared by session creation (sessionTagArgs) and the
// sidebar's verify/retag path so a new tag field cannot drift between them.
// Nil for an all-empty identity set: a marker without identity is refused.
// Display values are sanitized here so every writer emits the identical form.
func SessionTagPairs(tags SessionTags) []SessionTagPair {
	// Identity values are normalized the same way for every consumer:
	// strings are trimmed (whitespace-only is meaningless), timestamps are
	// positive-only (non-positive epochs are unset). The guard checks
	// normalized identity fields so a whitespace-only or display-only set
	// still refuses the bare marker.
	workspaceID := strings.TrimSpace(tags.WorkspaceID)
	tabID := strings.TrimSpace(tags.TabID)
	typ := strings.TrimSpace(tags.Type)
	assistant := strings.TrimSpace(tags.Assistant)
	instanceID := strings.TrimSpace(tags.InstanceID)
	owner := strings.TrimSpace(tags.SessionOwner)
	if workspaceID == "" && tabID == "" && typ == "" && assistant == "" && tags.CreatedAt <= 0 && instanceID == "" && owner == "" && tags.LeaseAtMS <= 0 {
		return nil
	}
	pairs := []SessionTagPair{{Key: "@amux", Value: "1"}}
	entries := []SessionTagPair{
		{Key: "@amux_workspace", Value: workspaceID},
		{Key: "@amux_tab", Value: tabID},
		{Key: "@amux_type", Value: typ},
		{Key: "@amux_assistant", Value: assistant},
		{Key: "@amux_created_at", Value: formatInt64Positive(tags.CreatedAt)},
		{Key: "@amux_instance", Value: instanceID},
		{Key: TagSessionOwner, Value: owner},
		{Key: TagSessionLeaseAt, Value: formatInt64Positive(tags.LeaseAtMS)},
		{Key: TagSessionOwnerHeartbeatAt, Value: formatInt64Positive(tags.LeaseAtMS)},
		// Display-only tags for external orchestrators — sanitized because
		// the project basename is filesystem-controlled (a workspace Name is
		// already validated to [a-zA-Z0-9._-], but the boundary strips
		// control bytes regardless).
		{Key: "@amux_workspace_name", Value: sanitizeTagValue(tags.WorkspaceName)},
		{Key: "@amux_project", Value: sanitizeTagValue(tags.ProjectName)},
	}
	for _, e := range entries {
		if e.Value != "" {
			pairs = append(pairs, e)
		}
	}
	return pairs
}

func sessionTagArgs(session string, tags SessionTags) [][]string {
	pairs := SessionTagPairs(tags)
	if pairs == nil {
		return nil
	}
	args := make([][]string, 0, len(pairs))
	for _, p := range pairs {
		value := p.Value
		if p.Key != "@amux" {
			value = shellutil.ShellQuote(value)
		}
		args = append(args, []string{"-t", session, p.Key, value})
	}
	return args
}

// tagValueMaxRunes bounds a display tag value; identity/timestamp values are
// fixed-format and never reach the sanitizer.
const tagValueMaxRunes = 128

// sanitizeTagValue strips unsafe display runes from a tag value and caps its
// length — the tmux-option twin of isUnsafeDisplayRune in
// ui/common/sanitize.go (kept local: tmux must not import the UI layer; the
// two lists must be updated together). It drops C0 (including newline/tab),
// DEL, C1, and the Unicode bidi/format controls (embed/override, isolates,
// LRM/RLM, ALM) that would let a stored value reorder or conceal rendered
// text — plus '|', the field separator used by list-sessions tag rows, so a
// stored value cannot shift the positional parse.
// Workspace names are already validated to a strict
// identifier charset; this guards the filesystem-controlled project basename.
func sanitizeTagValue(s string) string {
	var b strings.Builder
	written := 0
	for _, r := range s {
		if written >= tagValueMaxRunes {
			break
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || r == '|' ||
			(r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) ||
			r == 0x200e || r == 0x200f || r == 0x061c {
			continue
		}
		b.WriteRune(r)
		written++
	}
	return b.String()
}

func formatInt64Positive(v int64) string {
	if v <= 0 {
		return ""
	}
	return strconv.FormatInt(v, 10)
}
