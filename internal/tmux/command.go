package tmux

import (
	"fmt"
	"strconv"
	"strings"
)

// ClientCommandParams holds the parameters for building a tmux client command.
type ClientCommandParams struct {
	WorkDir        string
	Command        string
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
	return clientCommand(sessionName, p.WorkDir, p.Command, p.Options, p.Tags, p.DetachExisting)
}

func clientCommand(sessionName, workDir, command string, opts Options, tags SessionTags, detachExisting bool) string {
	base := tmuxBase(opts)
	session := shellQuote(sessionName)
	optionTgt := shellQuote(exactSessionOptionTarget(sessionName))
	sessionTgt := shellQuote(sessionTarget(sessionName))
	dir := shellQuote(workDir)
	// Strip tmux-specific vars inside managed panes so `tmux` commands do not
	// accidentally target the AMUX control server.
	command = "unset TMUX TMUX_PANE; " + command
	cmd := shellQuote(command)

	// Use atomic new-session -A to create/attach. Only pass -d when detaching others.
	detachFlag := ""
	if detachExisting {
		detachFlag = "d"
	}
	create := fmt.Sprintf("%s new-session -A%ss %s -c %s sh -lc %s",
		base, detachFlag, session, dir, cmd)

	var settings strings.Builder
	// Disable tmux prefix for this session only (not global) to make it transparent
	settings.WriteString(fmt.Sprintf("%s set-option -t %s prefix None 2>/dev/null; ", base, optionTgt))
	settings.WriteString(fmt.Sprintf("%s set-option -t %s prefix2 None 2>/dev/null; ", base, optionTgt))
	if opts.HideStatus {
		settings.WriteString(fmt.Sprintf("%s set-option -t %s status off 2>/dev/null; ", base, optionTgt))
	}
	if opts.DisableMouse {
		settings.WriteString(fmt.Sprintf("%s set-option -t %s mouse off 2>/dev/null; ", base, optionTgt))
	}
	if opts.DefaultTerminal != "" {
		settings.WriteString(fmt.Sprintf("%s set-option -t %s default-terminal %s 2>/dev/null; ", base, optionTgt, shellQuote(opts.DefaultTerminal)))
	}
	// Ensure activity timestamps update for window_activity-based tracking.
	settings.WriteString(fmt.Sprintf("%s set-option -t %s -w monitor-activity on 2>/dev/null; ", base, optionTgt))
	appendSessionTags(&settings, base, optionTgt, tags)

	// Attach to the session, optionally detaching other clients.
	attachFlag := "-t"
	if detachExisting {
		attachFlag = "-dt"
	}
	attach := fmt.Sprintf("%s attach %s %s", base, attachFlag, sessionTgt)

	return fmt.Sprintf("%s && %s%s", create, settings.String(), attach)
}

func appendSessionTags(settings *strings.Builder, base, session string, tags SessionTags) {
	if tags.WorkspaceID == "" && tags.TabID == "" && tags.Type == "" && tags.Assistant == "" && tags.CreatedAt == 0 && tags.InstanceID == "" && tags.SessionOwner == "" && tags.LeaseAtMS == 0 {
		return
	}
	settings.WriteString(fmt.Sprintf("%s set-option -t %s @amux 1 2>/dev/null; ", base, session))
	entries := []struct{ key, value string }{
		{"@amux_workspace", tags.WorkspaceID},
		{"@amux_tab", tags.TabID},
		{"@amux_type", tags.Type},
		{"@amux_assistant", tags.Assistant},
		{"@amux_created_at", formatInt64NonZero(tags.CreatedAt)},
		{"@amux_instance", tags.InstanceID},
		{TagSessionOwner, tags.SessionOwner},
		{TagSessionLeaseAt, formatInt64Positive(tags.LeaseAtMS)},
	}
	for _, e := range entries {
		if e.value != "" {
			settings.WriteString(fmt.Sprintf("%s set-option -t %s %s %s 2>/dev/null; ", base, session, e.key, shellQuote(e.value)))
		}
	}
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
