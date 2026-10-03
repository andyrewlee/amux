package panelaunch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Prepared owns one private launch attempt: a random `amux-pane-launch-*`
// directory under the temp root holding a single fixed-name payload file.
// The parent keeps the handle until the lifecycle outcome is proven — a
// never-started or provably-unsupervised attempt is Discard()ed, while an
// ambiguous one is left to expiry. Discard is idempotent.
type Prepared struct {
	dir  string
	path string

	once       sync.Once
	discardErr error
}

// Prepare validates the launch spec and writes it to a fresh private
// attempt. workDir need not exist yet (the consumer's chdir reports the
// failure inside the pane, matching the old cd trampoline), but the attempt
// directory must land outside the workspace so a managed worktree never
// carries launch secrets.
func Prepare(workDir, command string, env []string) (*Prepared, error) {
	p := &payload{
		Version:     protocolVersion,
		ExpiresUnix: nowFn().Add(launchBudget).Unix(),
		WorkDir:     []byte(workDir),
		Command:     []byte(command),
		Environment: make([][]byte, 0, len(env)),
	}
	for _, e := range env {
		if e == "" {
			continue
		}
		p.Environment = append(p.Environment, []byte(e))
	}
	if err := validateSpecBytes(p.WorkDir, p.Command, p.Environment); err != nil {
		return nil, err
	}
	raw, err := encodePayload(p)
	if err != nil {
		return nil, err
	}
	dir, err := createAttemptDir(workDir)
	if err != nil {
		return nil, err
	}
	if err := writePayload(dir, raw); err != nil {
		removeAttemptQuiet(dir)
		return nil, err
	}
	return &Prepared{dir: dir, path: filepath.Join(dir, payloadName)}, nil
}

// Path is the absolute payload path rendered into the pane invocation.
func (p *Prepared) Path() string { return p.path }

// Dir is the attempt directory (test introspection only).
func (p *Prepared) Dir() string { return p.dir }

// Discard removes this attempt's payload and its now-empty directory. Safe
// to call any number of times and after a consumer already ate the payload:
// only the exact owned artifacts are touched, a missing attempt is success,
// and a directory with foreign children is left alone.
func (p *Prepared) Discard() error {
	p.once.Do(func() {
		if err := checkAttemptDir(p.dir); err == nil {
			// Best effort: an unreadable-but-owned payload still expires.
			_ = removeOwnedPayload(p.dir)
		}
		if err := os.Remove(p.dir); err != nil && !os.IsNotExist(err) {
			p.discardErr = fmt.Errorf("pane launch: discard attempt: %w", err)
		}
	})
	return p.discardErr
}

// createAttemptDir makes a fresh 0700 attempt directory under the resolved
// temp root, refusing to land inside the launching workspace.
func createAttemptDir(workDir string) (string, error) {
	tmp := tempRootFn()
	tmpReal, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		tmpReal = tmp
	}
	wdReal, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		if abs, aerr := filepath.Abs(workDir); aerr == nil {
			wdReal = abs
		} else {
			wdReal = workDir
		}
	}
	dir, err := os.MkdirTemp(tmpReal, attemptPrefix)
	if err != nil {
		return "", fmt.Errorf("pane launch: create attempt dir: %w", err)
	}
	if insideDir(wdReal, dir) {
		removeAttemptQuiet(dir)
		return "", errors.New("pane launch: attempt dir inside workspace refused")
	}
	if err := checkAttemptDir(dir); err != nil {
		removeAttemptQuiet(dir)
		return "", err
	}
	return dir, nil
}

// insideDir reports whether child resolves under root.
func insideDir(root, child string) bool {
	rel, err := filepath.Rel(root, child)
	if err != nil || rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// writePayload creates the fixed-name 0600 payload file and verifies what
// landed: owned, regular, exact permissions.
func writePayload(dir string, raw []byte) error {
	path := filepath.Join(dir, payloadName)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("pane launch: create payload: %w", err)
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		removeAttemptQuiet(dir)
		return fmt.Errorf("pane launch: write payload: %w", err)
	}
	if err := f.Close(); err != nil {
		removeAttemptQuiet(dir)
		return fmt.Errorf("pane launch: write payload: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !euidOwns(info) {
		removeAttemptQuiet(dir)
		return stageOwnership
	}
	return nil
}

// removeAttemptQuiet tears down an attempt we created this Prepare call —
// the directory is fresh, randomly named, and contains at most our own
// payload, so removing it whole is exact rather than recursive damage.
func removeAttemptQuiet(dir string) {
	_ = os.RemoveAll(dir)
}
