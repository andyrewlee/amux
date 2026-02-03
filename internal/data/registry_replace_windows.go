//go:build windows

package data

import "os"

func replaceFile(tempPath, targetPath string) error {
	backupPath := targetPath + ".bak"
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(targetPath); err == nil {
		if err := os.Rename(targetPath, backupPath); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		if _, statErr := os.Stat(backupPath); statErr == nil {
			_ = os.Rename(backupPath, targetPath)
		}
		return err
	}
	_ = os.Remove(backupPath)
	return nil
}
