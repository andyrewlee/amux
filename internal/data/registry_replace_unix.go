//go:build !windows

package data

import "os"

func replaceFile(tempPath, targetPath string) error {
	return os.Rename(tempPath, targetPath)
}
