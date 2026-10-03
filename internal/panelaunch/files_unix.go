//go:build !windows

package panelaunch

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// euidOwns reports whether info describes a file owned by this process's
// effective user — the whole confidentiality boundary is same-user privacy.
func euidOwns(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Geteuid()
}

// checkAttemptDir verifies dir is an owned private attempt directory:
// correctly prefixed name, a real directory (Lstat, so a symlink fails),
// owned by us, and mode exactly 0700.
func checkAttemptDir(dir string) error {
	base := filepath.Base(dir)
	if len(base) <= len(attemptPrefix) || !strings.HasPrefix(base, attemptPrefix) {
		return stageOwnership
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return stageOwnership
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 || !euidOwns(info) {
		return stageOwnership
	}
	return nil
}

// openPayload opens the attempt's payload without following a final
// symlink and verifies the opened file is the exact file that passed the
// ownership check — a swap between Lstat and open cannot slip in a
// different file.
func openPayload(dir string) (*os.File, error) {
	path := filepath.Join(dir, payloadName)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !euidOwns(info) {
		return nil, stageOwnership
	}
	// #nosec G304 -- path is inside the validated private attempt dir.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	opened, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !os.SameFile(info, opened) {
		_ = f.Close()
		return nil, stageOwnership
	}
	return f, nil
}

// removeOwnedPayload unlinks the attempt's payload after re-verifying it is
// the expected owned regular 0600 file. A missing file is success (already
// consumed); a file failing the check is left in place and reported.
func removeOwnedPayload(dir string) error {
	path := filepath.Join(dir, payloadName)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() ||
		info.Mode().Perm() != 0o600 || !euidOwns(info) {
		return stageOwnership
	}
	return os.Remove(path)
}
