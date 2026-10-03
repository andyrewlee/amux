package panelaunch

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SweepExpired removes expired private launch attempts under root — the
// crash/cancel fallback when neither a consumer nor a discard reached an
// attempt. It touches only correctly named, owned directories whose latest
// checked modification time (attempt dir or payload) is older than the
// launch budget; fresh attempts, symlinks, files, foreign-owned entries,
// and directories carrying unrelated children are all left alone. warn may
// be nil; degradation is reported at the caller's Warn level, sanitized.
func SweepExpired(root string, now time.Time, warn func(format string, args ...any)) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if warn != nil {
			warn("pane launch sweep: cannot list temp root: %v", err)
		}
		return
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), attemptPrefix) {
			continue
		}
		sweepAttempt(filepath.Join(root, e.Name()), now, warn)
	}
}

// sweepAttempt applies the expiry rule to one candidate. A payload failing
// ownership makes the whole attempt off-limits — never unlink something we
// cannot prove is ours; the rmdir's empty-only semantics keep unrelated
// children safe.
func sweepAttempt(dir string, now time.Time, warn func(format string, args ...any)) {
	info, err := os.Lstat(dir)
	if err != nil {
		return // raced away or unreadable — nothing to prove
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !euidOwns(info) {
		return
	}
	latest := info.ModTime()
	payloadPath := filepath.Join(dir, payloadName)
	var payloadInfo os.FileInfo
	switch pi, err := os.Lstat(payloadPath); {
	case err == nil:
		if !pi.Mode().IsRegular() || !euidOwns(pi) {
			return // foreign or non-regular child — refuse the whole attempt
		}
		payloadInfo = pi
		if pi.ModTime().After(latest) {
			latest = pi.ModTime()
		}
	case os.IsNotExist(err):
		// Partial attempt (crashed writer or consumed payload) — the dir's
		// own mtime still bounds its age.
	default:
		return
	}
	if now.Before(latest.Add(launchBudget)) {
		return // fresh attempt — a consumer may still need it
	}
	if payloadInfo != nil {
		if err := os.Remove(payloadPath); err != nil && !os.IsNotExist(err) && warn != nil {
			warn("pane launch sweep: cannot remove expired payload: %v", err)
		}
	}
	if err := os.Remove(dir); err != nil && !os.IsNotExist(err) && warn != nil {
		// Non-empty attempts (unrelated children) land here; leaving them is
		// deliberate — a sweep never recursively deletes.
		warn("pane launch sweep: attempt dir left behind: %v", err)
	}
}
