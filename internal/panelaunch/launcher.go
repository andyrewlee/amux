package panelaunch

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// stableLauncherName is the fixed filename of the resilient launcher copy
// EnsureStableLauncher maintains inside the caller-chosen amux-owned dir.
const stableLauncherName = "amux-pane-launcher"

// Test seams — package-level so tests in this package can substitute fakes.
// Production callers must not override them.
var (
	executableFn = os.Executable
	linkFn       = os.Link
)

// stableMu guards stableDir: EnsureStableLauncher records it once at
// startup while later pane launches read it, and tests may interleave both.
var (
	stableMu  sync.RWMutex
	stableDir string
)

// EnsureStableLauncher publishes the running executable at
// dir/<stableLauncherName> so later pane launches survive the original
// binary path being deleted or replaced underneath a running amux — the
// dev-machine workflow (git clean, rebuild to a different path) that a
// pinned os.Executable() path cannot survive, and whose failure used to
// surface only as a distant "can't find session" attach error after the
// pane's instant death had already destroyed the session.
//
// Publication prefers a hardlink (free, and the inode outlives the original
// name) and falls back to a byte copy across filesystems. Replacement is
// atomic — staged under a temp name, then renamed over — so a concurrent
// pane exec never observes a half-written file, and a second amux instance
// refreshing the link only swaps in another build of the same helper; the
// payload protocol version gate covers that mix.
func EnsureStableLauncher(dir string) error {
	exe, err := executableFn()
	if err != nil {
		return fmt.Errorf("pane launch: resolve running binary: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("pane launch: create launcher dir: %w", err)
	}
	// MkdirAll applies the mode only to dirs it creates; the launcher dir is
	// amux-owned either way, so tighten a permissive pre-existing one.
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("pane launch: tighten launcher dir: %w", err)
	}
	final := filepath.Join(dir, stableLauncherName)
	publishStableDir(dir)
	if sameFile(exe, final) {
		return nil // already installed by this build (or a hardlink sibling)
	}
	tmp, err := os.CreateTemp(dir, stableLauncherName+".tmp-*")
	if err != nil {
		return fmt.Errorf("pane launch: stage launcher: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	_ = os.Remove(tmpPath) // link/copy below requires the staged name free
	if err := installLauncherFile(exe, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, final); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("pane launch: publish launcher: %w", err)
	}
	return nil
}

// StableLauncherPath returns the published stable launcher's path, or ""
// when EnsureStableLauncher has not successfully run in this process.
func StableLauncherPath() string {
	stableMu.RLock()
	defer stableMu.RUnlock()
	if stableDir == "" {
		return ""
	}
	return filepath.Join(stableDir, stableLauncherName)
}

// LauncherPath resolves the executable path a pane invocation embeds. The
// running binary's own path wins while it is still executable there (an
// in-place rebuild keeps launching the newer helper, preserving the
// historical semantics); the stable launcher takes over once that path is
// gone; with neither reachable, the error names the recovery instead of
// letting tmux surface the misleading attach failure that follows a pane
// that died before its session could matter.
func LauncherPath() (path string, viaStable bool, err error) {
	exe, err := executableFn()
	if err != nil {
		return "", false, fmt.Errorf("pane launch: resolve running binary: %w", err)
	}
	if fileExecutable(exe) {
		return exe, false, nil
	}
	if stable := StableLauncherPath(); stable != "" && fileExecutable(stable) {
		return stable, true, nil
	}
	return "", false, fmt.Errorf("pane launch: launcher binary %s no longer exists; amux was rebuilt or cleaned while running — restart amux", exe)
}

// installLauncherFile places the bytes of src at dst as a hardlink when the
// filesystem allows it, else as a copy synced to disk before the caller's
// rename publishes it (the fsatomic temp-write+fsync+rename contract — a
// crash after rename must never leave a truncated launcher, because the
// next startup's republish is best-effort). A running executable is
// readable on every supported platform; only concurrent writes are blocked
// (ETXTBSY), and nothing here writes the original. The hardlink path needs
// no sync: it publishes no new bytes.
func installLauncherFile(src, dst string) error {
	if err := linkFn(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src) // #nosec G304 -- src is the resolved running binary
	if err != nil {
		return fmt.Errorf("pane launch: open running binary: %w", err)
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("pane launch: stat running binary: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("pane launch: running binary is not a regular file")
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("pane launch: stage launcher copy: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("pane launch: copy launcher: %w", err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return fmt.Errorf("pane launch: sync launcher: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("pane launch: copy launcher: %w", err)
	}
	return nil
}

// publishStableDir records the directory LauncherPath falls back inside.
func publishStableDir(dir string) {
	stableMu.Lock()
	defer stableMu.Unlock()
	stableDir = dir
}

// fileExecutable reports whether path names a regular file this process
// could exec: Stat follows symlinks, so a store-path symlink whose target
// was garbage-collected reads as unusable, not as present.
func fileExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// sameFile reports whether two paths resolve to the same inode.
func sameFile(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ia, ib)
}
