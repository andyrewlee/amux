package panelaunch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withLauncherSeams swaps the launcher resolution seams for one test and
// restores them (plus the recorded stable dir) afterwards.
func withLauncherSeams(t *testing.T, exePath string) {
	t.Helper()
	origExe, origLink := executableFn, linkFn
	executableFn = func() (string, error) { return exePath, nil }
	t.Cleanup(func() {
		executableFn, linkFn = origExe, origLink
		stableMu.Lock()
		stableDir = ""
		stableMu.Unlock()
	})
}

// writeFakeExecutable writes an executable file the seams can stand in for
// the running binary. Rewriting it (remove + create) also gives it a fresh
// inode, which the hardlink-skip check distinguishes from a mere content
// change.
func writeFakeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake executable: %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod fake executable: %v", err)
	}
}

func TestEnsureStableLauncherPublishesAndRefreshes(t *testing.T) {
	src := filepath.Join(t.TempDir(), "amux")
	writeFakeExecutable(t, src, "v1")
	withLauncherSeams(t, src)

	dir := t.TempDir()
	if err := EnsureStableLauncher(dir); err != nil {
		t.Fatalf("EnsureStableLauncher() error = %v", err)
	}
	stable := filepath.Join(dir, stableLauncherName)
	got, err := os.ReadFile(stable)
	if err != nil {
		t.Fatalf("stable launcher missing: %v", err)
	}
	if string(got) != "v1" {
		t.Fatalf("stable launcher content = %q, want %q", got, "v1")
	}
	info, err := os.Stat(stable)
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("stable launcher not executable: %v %+v", err, info)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil || !dirInfo.IsDir() || dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("launcher dir mode = %+v err = %v, want 0700 dir", dirInfo, err)
	}
	if StableLauncherPath() != stable {
		t.Fatalf("StableLauncherPath() = %q, want %q", StableLauncherPath(), stable)
	}

	// A second install of a NEW build (fresh inode) must replace the link
	// content atomically.
	os.Remove(src)
	writeFakeExecutable(t, src, "v2")
	if err := EnsureStableLauncher(dir); err != nil {
		t.Fatalf("EnsureStableLauncher() refresh error = %v", err)
	}
	got, err = os.ReadFile(stable)
	if err != nil || string(got) != "v2" {
		t.Fatalf("stable launcher after refresh = %q (%v), want %q", got, err, "v2")
	}
}

func TestLauncherPathFallsBackToStableWhenExecutableDeleted(t *testing.T) {
	src := filepath.Join(t.TempDir(), "amux")
	writeFakeExecutable(t, src, "live")
	withLauncherSeams(t, src)

	if path, viaStable, err := LauncherPath(); err != nil || path != src || viaStable {
		t.Fatalf("LauncherPath() = (%q, %v, %v), want (%q, false, nil)", path, viaStable, err, src)
	}

	dir := t.TempDir()
	if err := EnsureStableLauncher(dir); err != nil {
		t.Fatalf("EnsureStableLauncher() error = %v", err)
	}
	stable := filepath.Join(dir, stableLauncherName)

	// The dev-machine failure: the original binary path is deleted (git
	// clean, rebuild elsewhere) while amux keeps running.
	if err := os.Remove(src); err != nil {
		t.Fatalf("remove original binary: %v", err)
	}
	path, viaStable, err := LauncherPath()
	if err != nil || path != stable || !viaStable {
		t.Fatalf("LauncherPath() after deletion = (%q, %v, %v), want (%q, true, nil)", path, viaStable, err, stable)
	}

	// A dangling symlink where the binary used to be (nix-style store path)
	// is equally unusable and must also fall back.
	dangling := filepath.Join(t.TempDir(), "amux-link")
	if err := os.Symlink(filepath.Join(t.TempDir(), "gone"), dangling); err != nil {
		t.Fatalf("symlink dangling: %v", err)
	}
	executableFn = func() (string, error) { return dangling, nil }
	if path, viaStable, err := LauncherPath(); err != nil || path != stable || !viaStable {
		t.Fatalf("LauncherPath() behind dangling symlink = (%q, %v, %v), want (%q, true, nil)", path, viaStable, err, stable)
	}

	// Neither path usable: the error must say what to do, not just name a
	// missing file.
	if err := os.Remove(stable); err != nil {
		t.Fatalf("remove stable launcher: %v", err)
	}
	_, _, err = LauncherPath()
	if err == nil || !strings.Contains(err.Error(), "restart amux") {
		t.Fatalf("LauncherPath() error = %v, want guidance to restart amux", err)
	}
}

func TestEnsureStableLauncherCopiesWhenLinkFails(t *testing.T) {
	src := filepath.Join(t.TempDir(), "amux")
	writeFakeExecutable(t, src, "payload-bytes")
	withLauncherSeams(t, src)
	origLink := linkFn
	linkFn = func(string, string) error { return errors.New("cross-device link") }
	t.Cleanup(func() { linkFn = origLink })

	dir := t.TempDir()
	if err := EnsureStableLauncher(dir); err != nil {
		t.Fatalf("EnsureStableLauncher() copy fallback error = %v", err)
	}
	stable := filepath.Join(dir, stableLauncherName)
	got, err := os.ReadFile(stable)
	if err != nil || string(got) != "payload-bytes" {
		t.Fatalf("copied launcher = %q (%v), want %q", got, err, "payload-bytes")
	}
	info, err := os.Stat(stable)
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("copied launcher not executable: %v %+v", err, info)
	}
	if sameFile(src, stable) {
		t.Fatal("copy fallback produced the same inode as the original; link seam not honored")
	}
}

func TestLauncherPathWithoutStableLauncherAndMissingExecutable(t *testing.T) {
	// No EnsureStableLauncher ran (StableLauncherPath == "") and the binary
	// path is gone: the actionable error, never an empty path.
	src := filepath.Join(t.TempDir(), "gone-already")
	withLauncherSeams(t, src)
	_, _, err := LauncherPath()
	if err == nil || !strings.Contains(err.Error(), "restart amux") {
		t.Fatalf("LauncherPath() error = %v, want guidance to restart amux", err)
	}
}
