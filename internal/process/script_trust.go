package process

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/fsatomic"
	"github.com/andyrewlee/amux/internal/logging"
)

// trustRegistryFilename is the basename of the per-user registry that records
// which repos' .amux/workspaces.json content the user has approved.
const trustRegistryFilename = "trusted-scripts.json"

// ErrScriptsNotTrusted is returned when a repo-supplied script would run
// without the user's approval of the config file. It is the sentinel callers
// test with errors.Is to distinguish a trust skip from a genuine setup failure.
var ErrScriptsNotTrusted = errors.New("project scripts not trusted")

// ErrScriptsChangedSincePrompt is returned when a user approves script content
// after the repo config changed from the content that originally triggered the prompt.
var ErrScriptsChangedSincePrompt = errors.New("project scripts changed since trust prompt")

// ErrTrustApprovalRequiresHash is returned when a hash-bound trust approval
// arrives without the hash of the content the user reviewed. The binding
// would be unverifiable, so the gate fails closed rather than degrading to an
// unbound trust of whatever happens to be on disk.
var ErrTrustApprovalRequiresHash = errors.New("trust approval requires the reviewed config hash")

// ScriptsNotTrustedError carries the hash of the repo config content that was
// blocked, so the UI can bind a later approval to the exact reviewed content.
type ScriptsNotTrustedError struct {
	Repo       string
	Command    string
	ConfigHash string
}

func (e *ScriptsNotTrustedError) Error() string {
	return fmt.Sprintf("%s (%q): %v", e.Repo, e.Command, ErrScriptsNotTrusted)
}

func (e *ScriptsNotTrustedError) Unwrap() error {
	return ErrScriptsNotTrusted
}

// ScriptTrust is the per-user registry of repos whose .amux/workspaces.json
// content the user has explicitly approved. It maps a normalized repo path to
// the hex SHA-256 of the config content that was approved, so any later edit to
// the repo's config invalidates the approval (the hash no longer matches).
// That hash pins only the .amux/workspaces.json content itself; it does not
// pin other repo files that an approved command may execute.
//
// The security property it enforces: repo-supplied executable config keys
// (config.SetupWorkspace / config.RunScript / config.ArchiveScript loaded from
// .amux/workspaces.json) never execute unless IsTrusted returns true. It is
// fail-closed — a missing or corrupt registry yields "not trusted". If a future
// change adds new repo-supplied executable config keys, they must go through the
// same IsTrusted check.
type ScriptTrust struct {
	path string
	mu   sync.Mutex
	// readFile is a per-instance test seam; nil means os.ReadFile.
	readFile func(string) ([]byte, error)
}

// NewScriptTrust returns a registry whose backing file lives in dir.
func NewScriptTrust(dir string) *ScriptTrust {
	return &ScriptTrust{path: filepath.Join(dir, trustRegistryFilename)}
}

// defaultScriptTrust returns a registry rooted at the amux home dir, resolved
// the same way internal/config resolves the data/config dir (no hardcoded
// ~/.amux). On any resolution error it returns an empty-path sentinel that
// never trusts and refuses to record approvals.
func defaultScriptTrust() *ScriptTrust {
	paths, err := config.DefaultPaths()
	if err != nil || paths == nil {
		logging.Warn("Could not resolve amux home for script trust registry: %v", err)
		return &ScriptTrust{path: ""}
	}
	return NewScriptTrust(paths.Home)
}

// hashConfig returns the hex SHA-256 of the config content.
func hashConfig(configContent []byte) string {
	sum := sha256.Sum256(configContent)
	return hex.EncodeToString(sum[:])
}

// scriptTrustFileVersion is the newest trusted-scripts.json schema written
// and readable. v0 = the pre-versioning bare map shape.
const scriptTrustFileVersion = 1

// scriptTrustFile is the versioned envelope written since v1. Repo keys are
// normalized absolute paths, so a top-level "version" key can never collide
// with a v0 entry — its presence means envelope.
type scriptTrustFile struct {
	Version int               `json:"version"`
	Trusted map[string]string `json:"trusted"`
}

// load reads the registry map. A missing file yields an empty map (no error);
// an unreadable, unparseable, or unknown-version file yields an empty map and
// a logged warning, so callers fail closed.
func (t *ScriptTrust) load() map[string]string {
	entries, err := t.loadStrict()
	if err == nil {
		return entries
	}
	switch {
	case errors.Is(err, data.ErrUnsupportedSchemaVersion):
		logging.Warn("Ignoring script trust registry %s: %v", t.path, err)
	case isScriptTrustDecodeErr(err):
		logging.Warn("Ignoring corrupt script trust registry %s: %v", t.path, err)
	default:
		logging.Warn("Could not read script trust registry %s: %v", t.path, err)
	}
	return map[string]string{}
}

// loadStrict reads the registry like load but returns classified errors so
// the write path can tell a missing file (empty state), corrupt JSON (the
// deliberate replacement-recovery path), a newer schema, or an unreadable
// file (refuse the write — never clobber bytes we could not inspect).
func (t *ScriptTrust) loadStrict() (map[string]string, error) {
	if t == nil || t.path == "" {
		return map[string]string{}, nil
	}
	read := os.ReadFile
	if t.readFile != nil {
		read = t.readFile
	}
	raw, err := read(t.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, err
	}
	if _, isEnvelope := probe["version"]; isEnvelope {
		// Decode the version first and refuse a supportedly-encoded future
		// integer before decoding `trusted` — a newer-format file is never
		// parsed leniently, that could manufacture trust from fields we
		// don't understand.
		var version int
		if err := json.Unmarshal(probe["version"], &version); err != nil {
			return nil, err
		}
		if version > scriptTrustFileVersion {
			return nil, fmt.Errorf("%w: trusted-scripts.json schema %d (newest known: %d)", data.ErrUnsupportedSchemaVersion, version, scriptTrustFileVersion)
		}
		var file scriptTrustFile
		if err := json.Unmarshal(raw, &file); err != nil {
			return nil, err
		}
		if file.Trusted == nil {
			return map[string]string{}, nil
		}
		return file.Trusted, nil
	}
	// v0: bare map shape.
	entries := map[string]string{}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	if entries == nil {
		// A valid top-level `null` decodes to nil — empty state, same as the
		// v1 envelope's nil-Trusted branch. Never an approval on its own.
		return map[string]string{}, nil
	}
	return entries, nil
}

// isScriptTrustDecodeErr reports whether err came from JSON decoding — the
// only failure mode where replacing existing registry bytes is the designed
// recovery.
func isScriptTrustDecodeErr(err error) bool {
	var synErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	return errors.As(err, &synErr) || errors.As(err, &typeErr)
}

// IsTrusted reports whether the user has approved the current content of
// repoPath's .amux/workspaces.json. It is fail-closed: a missing or corrupt
// registry, or any content change since approval, yields false.
func (t *ScriptTrust) IsTrusted(repoPath string, configContent []byte) bool {
	if t == nil {
		return false
	}
	key := data.NormalizePath(repoPath)
	if key == "" {
		return false
	}
	approved, ok := t.load()[key]
	if !ok {
		return false
	}
	return approved == hashConfig(configContent)
}

// Trust records configContent as the approved content for repoPath, writing the
// registry atomically (temp + fsync + rename) the same way the workspace store
// persists its JSON state.
func (t *ScriptTrust) Trust(repoPath string, configContent []byte) error {
	if t == nil {
		return nil
	}
	if t.path == "" {
		return errors.New("script trust registry unavailable: amux home could not be resolved")
	}
	key := data.NormalizePath(repoPath)
	if key == "" {
		// Mirror IsTrusted's guard: an empty key can never be matched, so
		// recording one would "succeed" while granting no real trust.
		return nil
	}
	// The in-process mutex plus the sibling flock serialize the whole
	// load→mutate→write: two Trust calls racing within or across processes
	// would otherwise read the same base map and silently lose one approval.
	t.mu.Lock()
	defer t.mu.Unlock()
	lockFile, err := fsatomic.LockFile(t.path+".lock", false)
	if err != nil {
		return err
	}
	defer fsatomic.UnlockFile(lockFile)
	entries, err := t.loadStrict()
	if err != nil {
		if isScriptTrustDecodeErr(err) {
			// Corrupt JSON is the designed recovery path: an explicit
			// approval is authoritative and replaces the file wholesale.
			entries = map[string]string{}
		} else {
			// A newer schema or an unreadable registry is existing state
			// we cannot inspect — refuse before creating directories or
			// writing so the existing bytes survive.
			return err
		}
	}
	entries[key] = hashConfig(configContent)

	if err := os.MkdirAll(filepath.Dir(t.path), 0o700); err != nil {
		return err
	}
	return fsatomic.WriteJSON(t.path, scriptTrustFile{
		Version: scriptTrustFileVersion,
		Trusted: entries,
	})
}

// ScriptsTrusted reports whether the repo's current .amux/workspaces.json
// content is approved — the read side of the trust gate for status surfaces.
// A repo with no config file reports true (nothing exists to gate).
func (r *ScriptRunner) ScriptsTrusted(repoPath string) (bool, error) {
	_, raw, err := r.loadConfigRaw(repoPath)
	if err != nil {
		return false, err
	}
	if raw == nil {
		return true, nil
	}
	return r.trust.IsTrusted(repoPath, raw), nil
}

// TrustRepoScripts records the current content of repoPath's
// .amux/workspaces.json as approved, so subsequent RunSetup/RunScript calls
// execute its repo-supplied commands. Approval is content-bound: any later edit
// to the file re-gates execution until the user trusts it again. A repo with no
// config file is a no-op (nothing to trust).
//
// Safety contract: this trusts whatever content is on disk at call time —
// there is no expected-hash check, so it is correct only for callers that did
// not show the user an approval prompt for specific content (test setup,
// non-interactive trust flows). Interactive trust MUST go through
// TrustRepoScriptsIfHash so a config edited between prompt and approval is
// rejected instead of silently trusted.
func (r *ScriptRunner) TrustRepoScripts(repoPath string) error {
	_, raw, err := r.loadConfigRaw(repoPath)
	if err != nil {
		return err
	}
	if raw == nil {
		return nil
	}
	return r.trust.Trust(repoPath, raw)
}

// TrustRepoScriptsIfHash records trust only if the repo config still matches the
// content hash that originally triggered the user approval prompt. An empty
// expectedHash is a fail-closed error: the approval would be unbound to any
// reviewed content, which is exactly the prompt→approval substitution the hash
// exists to prevent. Callers without a hash must re-prompt, not trust.
func (r *ScriptRunner) TrustRepoScriptsIfHash(repoPath, expectedHash string) error {
	_, raw, err := r.loadConfigRaw(repoPath)
	if err != nil {
		return err
	}
	if raw == nil {
		return nil
	}
	if expectedHash == "" {
		return ErrTrustApprovalRequiresHash
	}
	if hashConfig(raw) != expectedHash {
		return ErrScriptsChangedSincePrompt
	}
	return r.trust.Trust(repoPath, raw)
}

// Stop stops the running script for a workspace
