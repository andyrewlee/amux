package update

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/shellutil"
)

// renameFile is a seam for tests to inject rename failures.
var renameFile = os.Rename

type syncWriteCloser interface {
	io.WriteCloser
	Sync() error
}

var (
	maxExtractedBinaryBytes  int64 = 128 * 1024 * 1024
	maxExtractedArchiveBytes int64 = 256 * 1024 * 1024

	openCopySourceFile = func(name string) (io.ReadCloser, error) {
		return openFileReadInParentRoot(name)
	}
	openCopyDestFile = func(name string, flag int, perm os.FileMode) (syncWriteCloser, error) {
		return openFileInParentRoot(name, flag, perm)
	}
)

// openExtractFile is a seam for tests to inject extraction close failures.
var openExtractFile = func(name string, flag int, perm os.FileMode) (io.WriteCloser, error) {
	return openFileInParentRoot(name, flag, perm)
}

type extractedArchiveLimitReader struct {
	reader io.Reader
	max    int64
	read   int64
	probe  [1]byte
}

func (r *extractedArchiveLimitReader) Read(p []byte) (int, error) {
	if r.max < 0 {
		return 0, fmt.Errorf("archive exceeds %d byte uncompressed limit", r.max)
	}
	if r.read >= r.max {
		n, err := r.reader.Read(r.probe[:])
		if n > 0 {
			r.read += int64(n)
			return 0, fmt.Errorf("archive exceeds %d byte uncompressed limit", r.max)
		}
		return 0, err
	}

	remaining := r.max - r.read
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}

	n, err := r.reader.Read(p)
	r.read += int64(n)
	return n, err
}

// ExtractBinary extracts the amux binary from a tar.gz archive.
// Returns the path to the extracted binary.
func ExtractBinary(archivePath, destDir string) (string, error) {
	f, err := openFileReadInParentRoot(archivePath)
	if err != nil {
		return "", fmt.Errorf("opening archive: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("creating gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(&extractedArchiveLimitReader{
		reader: gzr,
		max:    maxExtractedArchiveBytes,
	})
	var binaryPath string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("reading tar: %w", err)
		}

		// Only extract the amux binary
		name := filepath.Base(header.Name)
		if name != "amux" {
			continue
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}
		if header.Size < 0 {
			return "", fmt.Errorf("amux binary has invalid size %d", header.Size)
		}
		if header.Size > maxExtractedBinaryBytes {
			return "", fmt.Errorf("amux binary exceeds %d byte limit", maxExtractedBinaryBytes)
		}

		binaryPath = filepath.Join(destDir, "amux")
		outFile, err := openExtractFile(binaryPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return "", fmt.Errorf("creating output file: %w", err)
		}

		if err := copyTarEntry(outFile, tr, header.Size); err != nil {
			_ = outFile.Close()
			return "", fmt.Errorf("extracting binary: %w", err)
		}
		if err := outFile.Close(); err != nil {
			return "", fmt.Errorf("closing extracted binary: %w", err)
		}
		break
	}

	if binaryPath == "" {
		return "", errors.New("amux binary not found in archive")
	}

	return binaryPath, nil
}

func copyTarEntry(w io.Writer, r io.Reader, size int64) error {
	var buf [32 * 1024]byte
	remaining := size
	for remaining > 0 {
		chunk := int64(len(buf))
		if remaining < chunk {
			chunk = remaining
		}

		n, readErr := r.Read(buf[:chunk])
		if n > 0 {
			written, writeErr := w.Write(buf[:n])
			if writeErr != nil {
				return writeErr
			}
			if written != n {
				return io.ErrShortWrite
			}
			remaining -= int64(n)
		}
		if readErr != nil {
			if readErr == io.EOF && remaining == 0 {
				return nil
			}
			return readErr
		}
		if n == 0 {
			return io.ErrNoProgress
		}
	}
	return nil
}

// InstallBinary performs an atomic replacement of the current binary. The new
// binary is staged in the target directory and the old binary is backed up by
// COPY — the live pathname never moves — so a kill at any point before the
// final rename leaves the old executable in place. The single same-directory
// rename over the existing path is the only commit step (atomic on the
// supported POSIX targets); directory syncs before and after persist the
// backup entry and the replaced entry.
func InstallBinary(newBinaryPath, currentBinaryPath string) error {
	// Ensure the new binary exists. The staged copy below is created with
	// executable permissions, so the source file itself does not need mutation.
	if _, err := os.Stat(newBinaryPath); err != nil {
		return fmt.Errorf("checking new binary: %w", err)
	}

	// Stage the new binary in the same directory as the target to avoid
	// cross-filesystem rename failures (EXDEV)
	targetDir := filepath.Dir(currentBinaryPath)
	stagedPath, err := uniqueUpgradeTempPath(targetDir, ".amux-upgrade-new-*")
	if err != nil {
		return fmt.Errorf("creating staging path: %w", err)
	}

	if err := copyFile(newBinaryPath, stagedPath); err != nil {
		return fmt.Errorf("staging new binary: %w", err)
	}
	defer os.Remove(stagedPath) // Clean up on failure

	// Copy the current binary to a backup path. A copy keeps the live
	// pathname occupied: every pre-commit failure below returns with the old
	// executable still reachable at currentBinaryPath.
	backupPattern := "." + filepath.Base(currentBinaryPath) + ".bak-*"
	backupPath, err := uniqueUpgradeTempPath(targetDir, backupPattern)
	if err != nil {
		return fmt.Errorf("creating backup path: %w", err)
	}
	if err := copyFile(currentBinaryPath, backupPath); err != nil {
		return fmt.Errorf("backing up current binary: %w", err)
	}
	keepBackup := false
	defer func() {
		if !keepBackup {
			_ = os.Remove(backupPath)
		}
	}()

	// Persist the backup's directory entry before committing the replacement.
	if err := syncInstallDir(targetDir); err != nil {
		return fmt.Errorf("syncing install directory before replacement: %w", err)
	}
	installPhaseHook("prepared")

	// The sole replacement: rename over the live pathname. A failure leaves
	// the old binary exactly where it was — there is nothing to restore.
	if err := renameFile(stagedPath, currentBinaryPath); err != nil {
		return fmt.Errorf("installing new binary: %w", err)
	}
	installPhaseHook("replaced")

	if err := syncInstallDir(targetDir); err != nil {
		// The new binary is already installed; keep the complete backup and
		// report it so a crash here can still be recovered manually.
		keepBackup = true
		return fmt.Errorf(
			"syncing install directory after replacement: %w (the new binary is already installed at %s; the previous binary was kept at %s — if the install is missing after a crash, restore it with: mv %s %s)",
			err, shellutil.ShellQuote(currentBinaryPath), shellutil.ShellQuote(backupPath),
			shellutil.ShellQuote(backupPath), shellutil.ShellQuote(currentBinaryPath),
		)
	}

	if err := os.Remove(backupPath); err != nil {
		keepBackup = true
		return fmt.Errorf(
			"removing backup %s: %w (the new binary is installed at %s; the backup was kept for manual recovery)",
			shellutil.ShellQuote(backupPath), err, shellutil.ShellQuote(currentBinaryPath),
		)
	}

	return nil
}

func uniqueUpgradeTempPath(dir, pattern string) (string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	if err := os.Remove(name); err != nil {
		return "", err
	}
	return name, nil
}

// copyFile copies a file from src to dst, preserving executable permissions.
// The destination is exclusively created: a refusal leaves a pre-existing
// file or symlink untouched and unowned, while any post-open failure removes
// the partial destination this call created.
func copyFile(src, dst string) error {
	srcFile, err := openCopySourceFile(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := openCopyDestFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	removePartial := true
	defer func() {
		if removePartial {
			_ = os.Remove(dst)
		}
	}()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		_ = dstFile.Close()
		return err
	}

	if err := dstFile.Sync(); err != nil {
		_ = dstFile.Close()
		return err
	}

	if err := dstFile.Close(); err != nil {
		return fmt.Errorf("closing destination file: %w", err)
	}
	removePartial = false

	return nil
}

// GetCurrentBinaryPath returns the path to the currently running binary.
func GetCurrentBinaryPath() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("getting executable path: %w", err)
	}

	// Resolve symlinks to get the actual binary path
	realPath, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		return "", fmt.Errorf("resolving symlinks: %w", err)
	}

	return realPath, nil
}

// IsGoInstall returns true if the binary appears to be installed via `go install`.
func IsGoInstall() bool {
	binPath, err := GetCurrentBinaryPath()
	if err != nil {
		return false
	}
	return isGoInstallPath(binPath)
}

func isGoInstallPath(binPath string) bool {
	home, _ := os.UserHomeDir()
	goPath := os.Getenv("GOPATH")
	if goPath == "" {
		goPath = filepath.Join(home, "go")
	}
	goBin := filepath.Join(goPath, "bin")
	rel, err := filepath.Rel(goBin, binPath)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

// CanWrite checks if we have write permission to the binary path.
func CanWrite(path string) bool {
	// Try to open for writing
	f, err := openFileInParentRoot(path, os.O_WRONLY, 0)
	if err == nil {
		_ = f.Close()
		return true
	}

	// Check if parent directory is writable (for rename operation)
	dir := filepath.Dir(path)
	f, err = os.CreateTemp(dir, ".amux-write-test-*")
	if err != nil {
		return false
	}
	testFile := f.Name()
	closeErr := f.Close()
	_ = os.Remove(testFile)
	return closeErr == nil
}
