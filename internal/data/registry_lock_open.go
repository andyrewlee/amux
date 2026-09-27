package data

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// lockOpenTransientRetries bounds the retry on a spurious ENOENT from the
// lock-file open. On macOS, openat-under-os.Root can intermittently report
// ENOENT on a live directory while sibling tempdir churn is in flight — a
// measured kernel/vnode-cache race, not a missing file. lockRegistryFile's
// mkdirAllPrivate already guarantees the directory exists, so an ENOENT here
// is transient by construction; a genuinely deleted state home still fails
// closed once the retries run out.
const lockOpenTransientRetries = 20

// openRegistryLockRootFn is the test seam over the real open — tests force
// transient ENOENT bursts to prove the retry converges.
var openRegistryLockRootFn = openRegistryLockRoot

// openRegistryLockRootRetry opens the lock root with a bounded retry on the
// spurious-ENOENT race documented above. All other errors return immediately.
func openRegistryLockRootRetry(lockPath string) (*os.Root, *os.File, string, error) {
	var root *os.Root
	var file *os.File
	var lockName string
	var err error
	for try := 0; try < lockOpenTransientRetries; try++ {
		root, file, lockName, err = openRegistryLockRootFn(lockPath)
		if !errors.Is(err, fs.ErrNotExist) {
			break
		}
		time.Sleep(time.Duration(try+1) * time.Millisecond)
	}
	return root, file, lockName, err
}

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
