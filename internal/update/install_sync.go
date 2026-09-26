package update

import (
	"os"
	"path/filepath"
	"runtime"
)

// syncInstallDir is a test seam over directory fsync so pre/post-replacement
// durability failures can be injected without host filesystem control.
var syncInstallDir = syncInstallDirImpl

// syncInstallDirImpl fsyncs a directory so entries renamed into it survive a
// crash. Mirrors internal/fsatomic's convention; Windows skips — interactive
// install targets are POSIX, and rename-over-existing isn't part of that
// runtime anyway.
func syncInstallDirImpl(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	parent := filepath.Dir(dir)
	name := filepath.Base(dir)
	if parent == dir {
		name = "."
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return err
	}
	f, openErr := root.Open(name)
	closeErr := root.Close()
	if openErr != nil {
		return openErr
	}
	if closeErr != nil {
		_ = f.Close()
		return closeErr
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// installPhaseHook is a test-only seam invoked at the two continuity-critical
// points of InstallBinary: "prepared" after the backup copy and pre-commit
// directory sync (immediately before the replacement rename) and "replaced"
// immediately after it. Production never reassigns it; helper-process
// interruption tests block inside it to prove which bytes a kill leaves
// behind.
var installPhaseHook = func(string) {}
