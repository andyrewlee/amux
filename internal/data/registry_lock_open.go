package data

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func openRegistryLockRoot(lockPath string) (*os.Root, *os.File, string, error) {
	dir := filepath.Dir(lockPath)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil, "", err
	}
	lockName := filepath.Base(lockPath)
	file, openErr := root.OpenFile(lockName, os.O_CREATE|os.O_RDWR, 0o600)
	if openErr != nil {
		return nil, nil, "", errors.Join(openErr, root.Close())
	}
	return root, file, lockName, nil
}

func mkdirAllPrivate(path string) error {
	rootPath, rel := splitRootPath(path)
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return err
	}
	mkdirErr := error(nil)
	if rel != "." {
		mkdirErr = root.MkdirAll(rel, 0o700)
	}
	closeErr := root.Close()
	if mkdirErr != nil {
		return errors.Join(mkdirErr, closeErr)
	}
	return closeErr
}

func splitRootPath(path string) (string, string) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return ".", clean
	}
	volume := filepath.VolumeName(clean)
	rest := strings.TrimPrefix(clean, volume)
	rootPath := volume + string(filepath.Separator)
	rel := strings.TrimPrefix(rest, string(filepath.Separator))
	if rel == "" {
		rel = "."
	}
	return rootPath, rel
}
