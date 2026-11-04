package tmux

import (
	"fmt"
	"regexp"
	"strings"
)

// The agent-facing input surface: `amux send` (Phase B) delivers text and
// named keys to a hosted session through tmux send-keys, encoding the
// input discipline docs/ORCHESTRATION.md teaches external orchestrators —
// exact-match targets, literal text, and a literal carriage return for
// submit (the named Enter key is wrong for raw-mode agents).

// sendKeyPattern is the closed key vocabulary `amux send --keys` accepts:
// only names tmux actually resolves as keys (verified byte-level against
// the dev-target tmux and its documented key table). tmux's unknown-key
// fallback silently types the argument as literal text with exit 0, so a
// name outside this set is not a soft failure — it is the wrong bytes in
// the pane. `cr` never reaches tmux (SendSessionKeys maps it to -H 0D).
// Anything else is refused up front: send-keys arguments are tmux syntax,
// so a free-form key name is an injection surface, not a key.
var sendKeyPattern = regexp.MustCompile(
	`^(?:cr|escape|space|tab|btab|up|down|left|right|home|end|pgup|pgdn|bspace|delete|insert|f(?:[1-9]|1[0-2])|[csm]-[a-zA-Z0-9=])$`)

// ValidSendKey reports whether name is an accepted key name for
// SendSessionKeys.
func ValidSendKey(name string) bool {
	return sendKeyPattern.MatchString(strings.TrimSpace(name))
}

// ExactSessionPaneTarget renders an exact-match PANE target for send-keys:
// `=name:` (exact session, its active window's active pane). tmux rejects
// a bare `=name` for pane targets ("can't find pane") — the exact-session
// `=` prefix only applies once the session part is delimited, which the
// trailing colon does. Prefix matching stays off: `-t =ws-tab-1:` cannot
// resolve onto `ws-tab-10`. Exported because it is the documented
// pane-target grammar of the external orchestration contract, pinned by
// session_contract_test.go.
func ExactSessionPaneTarget(sessionName string) string { return "=" + sessionName + ":" }

// SendSessionText sends literal text to a hosted session's active pane.
// The bytes arrive verbatim (-l): no tmux key translation, no trailing
// newline — submit is a separate SendSessionKeys call so a raw-mode agent
// never loses a CR fused onto text it has not read yet. The explicit `--`
// terminator matters: tmux's client parses argv itself, so a dash-leading
// payload ("-R", "--") would otherwise be consumed as a client flag —
// silently swallowed or, worse, interpreted (-R resets the pane terminal).
func SendSessionText(sessionName, text string, opts Options) error {
	if err := EnsureAvailable(); err != nil {
		return err
	}
	cmd, cancel := tmuxCommand(opts, "send-keys", "-t", ExactSessionPaneTarget(sessionName), "-l", "--", text)
	defer cancel()
	if _, err := runTmuxCmd(cmd); err != nil {
		return fmt.Errorf("tmux: send text to %s: %w", sessionName, err)
	}
	return nil
}

// SendSessionKeys sends named keys (see ValidSendKey) to a hosted
// session's active pane as one tmux command, so an ordered sequence
// arrives ordered. `cr` is delivered as a literal carriage return
// (send-keys -H 0D): raw-mode agent TUIs never see the named Enter key
// translated, and the close-loop e2e suite pins CR as the submit byte.
func SendSessionKeys(sessionName string, keys []string, opts Options) error {
	if err := EnsureAvailable(); err != nil {
		return err
	}
	names := make([]string, 0, len(keys))
	for _, key := range keys {
		trimmed := strings.TrimSpace(key)
		if !ValidSendKey(trimmed) {
			return fmt.Errorf("tmux: refusing to send unknown key %q", trimmed)
		}
		names = append(names, trimmed)
	}
	if len(names) == 0 {
		return nil
	}
	// One chained tmux command: the bare ";" argv separator is exec
	// semantics (each element verbatim, no shell), the same chain shape
	// WritePaneReport uses.
	argv := make([]string, 0, len(names)*7)
	for i, key := range names {
		if i > 0 {
			argv = append(argv, ";")
		}
		if key == "cr" {
			argv = append(argv, "send-keys", "-t", ExactSessionPaneTarget(sessionName), "-H", "0D")
		} else {
			// `--` for the same reason as SendSessionText: the vocabulary
			// already excludes flag-shaped names, but the terminator keeps
			// that invariant structural rather than regex-dependent.
			argv = append(argv, "send-keys", "-t", ExactSessionPaneTarget(sessionName), "--", key)
		}
	}
	cmd, cancel := tmuxCommand(opts, argv...)
	defer cancel()
	if _, err := runTmuxCmd(cmd); err != nil {
		return fmt.Errorf("tmux: send keys to %s: %w", sessionName, err)
	}
	return nil
}
