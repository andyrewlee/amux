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
)

// ExtractBinary extracts the amux binary from a tar.gz archive.
// Returns the path to the extracted binary.
func ExtractBinary(archivePath, destDir string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("opening archive: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("creating gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
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

		binaryPath = filepath.Join(destDir, "amux")
		outFile, err := os.OpenFile(binaryPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return "", fmt.Errorf("creating output file: %w", err)
		}

		if _, err := io.Copy(outFile, tr); err != nil {
			outFile.Close()
			return "", fmt.Errorf("extracting binary: %w", err)
		}
		outFile.Close()
		break
	}

	if binaryPath == "" {
		return "", errors.New("amux binary not found in archive")
	}

	return binaryPath, nil
}

// InstallBinary performs an atomic replacement of the current binary.
// It stages the new binary in the same directory as the target to avoid
// cross-filesystem rename issues, then uses rename to atomically swap.
func InstallBinary(newBinaryPath, currentBinaryPath string) error {
	// Ensure the new binary exists and is executable
	info, err := os.Stat(newBinaryPath)
	if err != nil {
		return fmt.Errorf("checking new binary: %w", err)
	}
	if info.Mode()&0o111 == 0 {
		if err := os.Chmod(newBinaryPath, 0o755); err != nil {
			return fmt.Errorf("setting executable permission: %w", err)
		}
	}

	// Stage the new binary in the same directory as the target to avoid
	// cross-filesystem rename failures (EXDEV)
	targetDir := filepath.Dir(currentBinaryPath)
	stagedPath := filepath.Join(targetDir, ".amux-upgrade-new")

	if err := copyFile(newBinaryPath, stagedPath); err != nil {
		return fmt.Errorf("staging new binary: %w", err)
	}
	defer os.Remove(stagedPath) // Clean up on failure

	// Create backup of current binary
	backupPath := currentBinaryPath + ".bak"
	if err := os.Rename(currentBinaryPath, backupPath); err != nil {
		return fmt.Errorf("backing up current binary: %w", err)
	}

	// Atomically replace with staged binary (same filesystem, so rename works)
	if err := os.Rename(stagedPath, currentBinaryPath); err != nil {
		// Try to restore backup
		_ = os.Rename(backupPath, currentBinaryPath)
		return fmt.Errorf("installing new binary: %w", err)
	}

	// Remove backup
	_ = os.Remove(backupPath)

	return nil
}

// copyFile copies a file from src to dst, preserving executable permissions.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return dstFile.Sync()
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

	// Typical go install path: $GOPATH/bin or $HOME/go/bin
	home, _ := os.UserHomeDir()
	goPath := os.Getenv("GOPATH")
	if goPath == "" {
		goPath = filepath.Join(home, "go")
	}

	goBin := filepath.Join(goPath, "bin")
	return strings.HasPrefix(binPath, goBin)
}

// CanWrite checks if we have write permission to the binary path.
func CanWrite(path string) bool {
	// Try to open for writing
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		f.Close()
		return true
	}

	// Check if parent directory is writable (for rename operation)
	dir := filepath.Dir(path)
	testFile := filepath.Join(dir, ".amux-write-test")
	f, err = os.Create(testFile)
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(testFile)
	return true
}
