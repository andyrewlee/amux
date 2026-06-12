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
	hadPrimary, hadBackupOnly, err := prepareRetryMarkerWindowsBackup(path, backupPath)
	if err != nil {
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

func prepareRetryMarkerWindowsBackup(path, backupPath string) (hadPrimary, hadBackupOnly bool, err error) {
	if _, err := os.Stat(path); err == nil {
		if err := writeRetryMarkerRemovePath(backupPath); err != nil && !os.IsNotExist(err) {
			return false, false, err
		}
		if err := writeRetryMarkerRenamePath(path, backupPath); err != nil {
			return false, false, err
		}
		return true, false, nil
	} else if !os.IsNotExist(err) {
		return false, false, err
	}
	if _, err := os.Stat(backupPath); err == nil {
		return false, true, nil
	} else if !os.IsNotExist(err) {
		return false, false, err
	}
	return false, false, nil
}
