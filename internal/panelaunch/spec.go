// Package panelaunch transports a managed pane's captured launch spec —
// working directory, shell command, and environment assignments — to the pane
// process through a short-lived private file instead of shell command
// arguments. The parent renders a pane invocation of exactly
//
//	'<launcher>' --internal-pane-launch run '<payload-path>'
//
// so no environment value ever appears in argv, tmux command arguments, or
// the stored pane_start_command. The same amux executable consumes the
// payload inside the pane: it validates private ownership and expiry, unlinks
// the payload, merges the assignments over its inherited environment,
// chdirs, and syscall.Exec's the final `sh -lc` command.
//
// The trust boundary is same-user local access: the payload lives in a
// per-attempt 0700 directory holding a 0600 file and is consumable for five
// minutes. This hides values from command-line surfaces; it does not hide a
// process's environment from its own user or from privileged inspection.
package panelaunch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// protocolVersion is the private payload format version. Rejected on
	// mismatch — never silently upgrade an unexpected structure.
	protocolVersion = 1

	// attemptPrefix names every per-attempt directory under the temp root.
	attemptPrefix = "amux-pane-launch-"
	// payloadName is the fixed filename inside an attempt directory.
	payloadName = "payload"
	// invocationFlag is the reserved private argv prefix.
	invocationFlag = "--internal-pane-launch"

	// maxPayloadBytes bounds a payload read; a real launch spec is a few KB.
	maxPayloadBytes = 1 << 20

	// exitFailure reports a validated-invocation stage failure.
	exitFailure = 1
	// exitUsage reports a malformed invocation shape.
	exitUsage = 2
)

// launchBudget is the handoff window: a payload stays consumable for five
// minutes after preparation, covering delayed pane creation, create races,
// and interrupted parents. It bounds consumability — physical deletion still
// requires the consumer, a discard, or a later sweep.
var launchBudget = 5 * time.Minute

// Test seams — package-level so tests in this package can substitute fakes.
// Production callers must not override them.
var (
	nowFn      = time.Now
	tempRootFn = os.TempDir
)

// payload is the decoded private launch spec. Byte slices survive the JSON
// round-trip verbatim (base64 encoding), so non-UTF-8 env values, unusual
// env names, and multiline commands cannot be mangled by a string codec.
type payload struct {
	Version     int      `json:"v"`
	ExpiresUnix int64    `json:"exp"`
	WorkDir     []byte   `json:"workdir"`
	Command     []byte   `json:"command"`
	Environment [][]byte `json:"env"`
}

// stageError is a sanitized failure: the stage name is the whole story.
// Payload contents — command text, env names/values, decoder excerpts —
// must never reach a returned error or stderr.
type stageError string

func (e stageError) Error() string { return "pane launch: " + string(e) }

const (
	stageInvocation stageError = "invalid invocation"
	stageOwnership  stageError = "ownership check failed"
	stagePayload    stageError = "invalid payload"
	stageDirectory  stageError = "directory check failed"
	stageShell      stageError = "shell resolution failed"
	stageExec       stageError = "exec failed"
	stageExpired    stageError = "payload expired"
)

// encodePayload marshals the spec for file storage.
func encodePayload(p *payload) ([]byte, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, stagePayload
	}
	return raw, nil
}

// decodePayload parses and validates raw bytes at the given time. Every
// rejection is a stage error with no content detail.
func decodePayload(raw []byte, now time.Time) (*payload, error) {
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, stagePayload
	}
	if p.Version != protocolVersion {
		return nil, stagePayload
	}
	if now.Unix() >= p.ExpiresUnix {
		return nil, stageExpired
	}
	if err := validateSpecBytes(p.WorkDir, p.Command, p.Environment); err != nil {
		return nil, err
	}
	return &p, nil
}

// validateSpecBytes enforces the launch-spec shape shared by encode and
// decode: no NUL bytes anywhere (they would corrupt argv/env on exec), a
// non-empty working directory, and environment entries of NAME=VALUE form
// with a non-empty name. Env names are not restricted to POSIX identifier
// characters — the kernel accepts anything without '=' or NUL and existing
// delivery did too.
func validateSpecBytes(workDir, command []byte, env [][]byte) error {
	if len(workDir) == 0 || containsNUL(workDir) || containsNUL(command) {
		return stagePayload
	}
	for _, e := range env {
		if containsNUL(e) {
			return stagePayload
		}
		name, _, ok := cutEntry(e)
		if !ok || len(name) == 0 {
			return stagePayload
		}
	}
	return nil
}

func containsNUL(b []byte) bool {
	for _, c := range b {
		if c == 0 {
			return true
		}
	}
	return false
}

// cutEntry splits an environment assignment on its first '='.
func cutEntry(e []byte) (name, value []byte, ok bool) {
	for i, c := range e {
		if c == '=' {
			return e[:i], e[i+1:], true
		}
	}
	return nil, nil, false
}

// envName returns the name half of a "NAME=VALUE" string.
func envName(entry string) string {
	if i := strings.IndexByte(entry, '='); i >= 0 {
		return entry[:i]
	}
	return entry
}

// mergeEnv applies assignments over base with last-assignment-wins
// semantics, matching the previous `env K=V ...` delivery: server-inherited
// keys survive unless a captured assignment names them, duplicate
// assignments resolve to the last one, and empty values stay empty. Order is
// first-appearance position so the merged environment is stable.
func mergeEnv(base []string, assignments [][]byte) []string {
	pos := make(map[string]int, len(base)+len(assignments))
	out := make([]string, 0, len(base)+len(assignments))
	put := func(entry string) {
		name := envName(entry)
		if i, ok := pos[name]; ok {
			out[i] = entry
			return
		}
		pos[name] = len(out)
		out = append(out, entry)
	}
	for _, e := range base {
		put(e)
	}
	for _, a := range assignments {
		put(string(a))
	}
	return out
}

// dropEnvKeys returns env without the named keys (the pane's tmux-injected
// TMUX/TMUX_PANE, which the old in-shell `unset` also removed).
func dropEnvKeys(env []string, keys ...string) []string {
	out := env[:0]
	for _, e := range env {
		name := envName(e)
		drop := false
		for _, k := range keys {
			if name == k {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, e)
		}
	}
	return out
}

// HandleInvocation runs the private pane-launch helper protocol. It handles
// exactly `--internal-pane-launch run <abs-payload>` and
// `--internal-pane-launch discard <abs-payload>`; any other argv is left to
// the caller's normal dispatch. The reserved flag with malformed arguments
// is a handled generic failure, so a stray helper argv can never fall
// through into TUI startup.
func HandleInvocation(args []string) (handled bool, exitCode int) {
	if len(args) == 0 || args[0] != invocationFlag {
		return false, 0
	}
	if len(args) != 3 || (args[1] != "run" && args[1] != "discard") {
		fmt.Fprintln(os.Stderr, stageInvocation.Error())
		return true, exitUsage
	}
	if args[1] == "run" {
		return true, runPayload(args[2])
	}
	return true, discardPayload(args[2])
}

// InvocationArgv returns the helper argv (launcher path plus private flag,
// operation, and payload path) that a pane command embeds. Rendering and
// quoting belong to the caller so this stays the single shape definition.
func InvocationArgv(launcher, op, payloadPath string) []string {
	return []string{launcher, invocationFlag, op, payloadPath}
}

// validateInvocationPath enforces the on-disk identity a helper may touch:
// an absolute path whose final element is the fixed payload name inside a
// correctly prefixed attempt directory.
func validateInvocationPath(path string) (dir string, err error) {
	if !filepath.IsAbs(path) || filepath.Base(path) != payloadName {
		return "", stageInvocation
	}
	dir = filepath.Dir(path)
	if !strings.HasPrefix(filepath.Base(dir), attemptPrefix) {
		return "", stageInvocation
	}
	return dir, nil
}

// stageFail reports a generic stage failure on stderr. No payload content,
// path, or system error detail is printed — the pane's stderr can be
// captured by the same surfaces this transport exists to keep clean.
func stageFail(stage stageError) int {
	fmt.Fprintln(os.Stderr, stage.Error())
	return exitFailure
}
