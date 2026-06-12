package git

import (
	"os"
	"path/filepath"
	"runtime"
)

var (
	writeRetryMarkerRenamePath = os.Rename
	writeRetryMarkerRemovePath = os.Remove
)

func writeRetryMarkerFileAtomically(path string, payload []byte, perm os.FileMode) error {
	return writeRetryMarkerFileAtomicallyForGOOS(runtime.GOOS, path, payload, perm)
}

func writeRetryMarkerFileAtomicallyForGOOS(goos, path string, payload []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tempFile, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()

	if err := tempFile.Chmod(perm); err != nil {
		_ = tempFile.Close()
		return err
	}
	if _, err := tempFile.Write(payload); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if goos == "windows" {
		if err := replaceFileForWindows(path, tempPath); err != nil {
			return err
		}
	} else if err := writeRetryMarkerRenamePath(tempPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func replaceFileForWindows(path, tempPath string) error {
	backupPath := retryMarkerBackupPath(path)
	hadPrimary := false
	hadBackupOnly := false
	if _, err := os.Stat(path); err == nil {
		hadPrimary = true
		if err := writeRetryMarkerRemovePath(backupPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := writeRetryMarkerRenamePath(path, backupPath); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	} else if _, err := os.Stat(backupPath); err == nil {
		hadBackupOnly = true
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := writeRetryMarkerRenamePath(tempPath, path); err != nil {
		if hadPrimary {
			_ = writeRetryMarkerRenamePath(backupPath, path)
		}
		return err
	}
	if hadPrimary || hadBackupOnly {
		_ = writeRetryMarkerRemovePath(backupPath)
	}
	return nil
}
