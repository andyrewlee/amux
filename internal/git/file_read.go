package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func readFileInParentRoot(path string) ([]byte, error) {
	root, name, err := openParentRoot(path)
	if err != nil {
		return nil, err
	}
	data, readErr := root.ReadFile(name)
	if err := closeParentRoot(root, readErr); err != nil {
		return nil, err
	}
	return data, nil
}

func statAndReadFileInParentRoot(path string) (os.FileInfo, []byte, error) {
	root, name, err := openParentRoot(path)
	if err != nil {
		return nil, nil, err
	}
	info, statErr := root.Stat(name)
	if statErr != nil {
		return nil, nil, closeParentRoot(root, statErr)
	}
	data, readErr := root.ReadFile(name)
	if err := closeParentRoot(root, readErr); err != nil {
		return nil, nil, err
	}
	return info, data, nil
}

func openParentRoot(path string) (*os.Root, string, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, "", err
	}
	return root, filepath.Base(path), nil
}

func closeParentRoot(root *os.Root, opErr error) error {
	closeErr := root.Close()
	if opErr != nil {
		if closeErr != nil {
			return errors.Join(opErr, fmt.Errorf("close file parent directory: %w", closeErr))
		}
		return opErr
	}
	if closeErr != nil {
		return fmt.Errorf("close file parent directory: %w", closeErr)
	}
	return nil
}
