// Package fsatomic provides crash-safe single-file persistence: data is
// written to a temp file in the target directory, synced to disk, and then
// atomically moved over the target. On Windows, where rename-over-existing is
// not atomic, the existing file is shuffled to a .bak first and restored on
// failure; readers that care about mid-replace crashes can fall back to that
// .bak.
package fsatomic

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

var (
	// renameFile is a test seam over os.Rename so the rename-failure restore
	// branch can be exercised. Production never reassigns it.
	renameFile = os.Rename

	// syncParentDir is a test seam over directory fsync so durability and
	// failure handling can be exercised without relying on host filesystems.
	syncParentDir = syncDir
)

// WriteFile atomically replaces path with data. The temp file is created in
// path's directory so the final rename never crosses filesystems. On POSIX,
// both the file and the parent directory are synced so the renamed directory
// entry is durable after return. Replacement files keep os.CreateTemp's private
// permissions; perm is accepted for parity with os.WriteFile but is never used
// to widen access beyond the process umask.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	return writeFileForGOOS(runtime.GOOS, path, data, perm)
}

// WriteJSON marshals v as two-space-indented JSON and atomically writes it to
// path via WriteFile (temp + fsync + atomic rename). The indent matches the
// on-disk format amux uses for every metadata file, so callers no longer repeat
// the marshal-then-WriteFile dance.
func WriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("fsatomic: marshal %s: %w", path, err)
	}
	return WriteFile(path, data, 0o644)
}

func writeFileForGOOS(goos, path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if goos == "windows" {
		if err := replaceFileWindows(path, tmpPath); err != nil {
			return err
		}
	} else if err := renameFile(tmpPath, path); err != nil {
		return err
	}
	if err := syncParentDirForGOOS(goos, dir); err != nil {
		return fmt.Errorf("sync directory %s: %w", dir, err)
	}
	cleanup = false
	return nil
}

func syncParentDirForGOOS(goos, dir string) error {
	if goos == "windows" {
		return nil
	}
	return syncParentDir(dir)
}

func syncDir(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// replaceFileWindows replaces path with tmpPath via a backup shuffle:
// existing file → .bak, temp → path, then the .bak is removed. On a failed
// final rename the backup is restored so the previous contents survive.
func replaceFileWindows(path, tmpPath string) error {
	backupPath := path + ".bak"
	hadPrimary := false
	if _, err := os.Stat(path); err == nil {
		hadPrimary = true
		if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := renameFile(path, backupPath); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := renameFile(tmpPath, path); err != nil {
		if hadPrimary {
			_ = renameFile(backupPath, path)
		}
		return err
	}
	_ = os.Remove(backupPath)
	return nil
}
