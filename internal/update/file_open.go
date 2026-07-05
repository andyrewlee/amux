package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func openFileReadInParentRoot(path string) (*os.File, error) {
	return openFileInParentRoot(path, os.O_RDONLY, 0)
}

func openFileInParentRoot(path string, flag int, perm os.FileMode) (*os.File, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	file, openErr := root.OpenFile(filepath.Base(path), flag, perm)
	closeErr := root.Close()
	if openErr != nil {
		if closeErr != nil {
			return nil, errors.Join(openErr, fmt.Errorf("close file parent directory: %w", closeErr))
		}
		return nil, openErr
	}
	if closeErr != nil {
		_ = file.Close()
		return nil, fmt.Errorf("close file parent directory: %w", closeErr)
	}
	return file, nil
}
