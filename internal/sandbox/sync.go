package sandbox

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var defaultIgnorePatterns = []string{
	"node_modules",
	".next",
	"dist",
	"build",
	".turbo",
	".amux",
}

const (
	uploadTarPath         = "/tmp/amux-upload.tgz"
	downloadTarPath       = "/tmp/amux-download.tgz"
	workspaceBaseDir      = ".amux/workspaces"
	maxBufferDownloadSize = 100 * 1024 * 1024
	timeoutSeconds        = 300
)

// SyncOptions configures workspace sync.
type SyncOptions struct {
	Cwd            string
	WorktreeID     string
	IncludeGit     bool
	IgnorePatterns []string
}

func shouldIgnoreFile(filePath string, ignorePatterns []string) bool {
	if filePath == "" || filePath == "." {
		return false
	}
	parts := strings.Split(filepath.ToSlash(filePath), "/")
	for _, part := range parts {
		for _, pattern := range ignorePatterns {
			if part == pattern {
				return true
			}
		}
	}
	return false
}

func getStdout(resp *ExecResult) string {
	if resp == nil {
		return ""
	}
	return resp.Stdout
}

func assertCommandSuccess(resp *ExecResult, context string) error {
	if resp == nil {
		return fmt.Errorf("%s (no response)", context)
	}
	if resp.ExitCode != 0 {
		stdout := strings.TrimSpace(getStdout(resp))
		details := ""
		if stdout != "" {
			details = ": " + stdout
		}
		return fmt.Errorf("%s (exit %d)%s", context, resp.ExitCode, details)
	}
	return nil
}

func getSandboxHomeDirForSync(computer RemoteSandbox) string {
	resp, err := execCommand(computer, "echo $HOME", nil)
	if err == nil {
		stdout := strings.TrimSpace(getStdout(resp))
		if stdout != "" {
			return stdout
		}
	}
	return "/home/daytona"
}

func resolveWorkspaceRepoPath(computer RemoteSandbox, worktreeID string) string {
	homeDir := getSandboxHomeDirForSync(computer)
	return path.Join(homeDir, workspaceBaseDir, worktreeID, "repo")
}

func parseSizeFromOutput(output string) int64 {
	fields := strings.Fields(output)
	if len(fields) == 0 {
		return -1
	}
	var size int64
	_, err := fmt.Sscanf(fields[0], "%d", &size)
	if err != nil {
		return -1
	}
	return size
}

func getRemoteFileSize(computer RemoteSandbox, remotePath string) int64 {
	resp, err := execCommand(computer, fmt.Sprintf("stat -c %%s %s 2>/dev/null || wc -c < %s", remotePath, remotePath), nil)
	if err != nil || resp.ExitCode != 0 {
		return -1
	}
	return parseSizeFromOutput(getStdout(resp))
}

func ensureGzipFile(localPath string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()
	buf := make([]byte, 4)
	if _, err := io.ReadFull(file, buf); err != nil {
		return err
	}
	if buf[0] == 0x1f && buf[1] == 0x8b {
		return nil
	}
	preview, _ := os.ReadFile(localPath)
	snippet := strings.TrimSpace(string(preview))
	if len(snippet) > 200 {
		snippet = snippet[:200]
	}
	if snippet != "" {
		return fmt.Errorf("downloaded archive is invalid. First bytes: %s", snippet)
	}
	return fmt.Errorf("downloaded archive is invalid")
}

func isArchiveError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "gzip") || strings.Contains(msg, "tar") || strings.Contains(msg, "archive")
}

func getIgnorePatterns(opts SyncOptions) ([]string, error) {
	patterns := append([]string{}, defaultIgnorePatterns...)
	if len(opts.IgnorePatterns) > 0 {
		patterns = append(patterns, opts.IgnorePatterns...)
	}
	ignoreFiles := []string{".amuxignore"}
	for _, name := range ignoreFiles {
		ignorePath := filepath.Join(opts.Cwd, name)
		if data, err := os.ReadFile(ignorePath); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				patterns = append(patterns, line)
			}
		}
	}
	if !opts.IncludeGit {
		patterns = append(patterns, ".git")
	}
	return patterns, nil
}

func createTarball(opts SyncOptions) (string, error) {
	patterns, err := getIgnorePatterns(opts)
	if err != nil {
		return "", err
	}
	file, err := os.CreateTemp("", "amux-upload-*.tgz")
	if err != nil {
		return "", err
	}
	defer file.Close()
	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	walkFn := func(pathname string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(opts.Cwd, pathname)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if shouldIgnoreFile(rel, patterns) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		linkTarget := ""
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, _ = os.Readlink(pathname)
		}
		hdr, err := tar.FileInfoHeader(info, linkTarget)
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if entry.IsDir() {
			hdr.Name += "/"
		}
		if err := tarWriter.WriteHeader(hdr); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			file, err := os.Open(pathname)
			if err != nil {
				return err
			}
			_, err = io.Copy(tarWriter, file)
			_ = file.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}

	if err := filepath.WalkDir(opts.Cwd, walkFn); err != nil {
		return "", err
	}
	return file.Name(), nil
}

// UploadWorkspace syncs local workspace to a sandbox.
func UploadWorkspace(computer RemoteSandbox, opts SyncOptions, verbose bool) error {
	if verbose {
		fmt.Println("Creating tarball of local workspace...")
	}
	tarPath, err := createTarball(opts)
	if err != nil {
		return err
	}
	defer os.Remove(tarPath)
	if verbose {
		fmt.Printf("Uploading to %s in sandbox...\n", uploadTarPath)
	}
	if err := computer.UploadFile(context.Background(), tarPath, uploadTarPath); err != nil {
		return err
	}
	if verbose {
		fmt.Println("Upload complete")
	}

	repoPath := resolveWorkspaceRepoPath(computer, opts.WorktreeID)
	_, _ = execCommand(computer, SafeCommands.RmRf(repoPath), nil)
	_, _ = execCommand(computer, SafeCommands.MkdirP(repoPath), nil)
	resp, err := execCommand(computer, SafeCommands.TarExtract(uploadTarPath, repoPath), nil)
	if err != nil {
		return err
	}
	if err := assertCommandSuccess(resp, "failed to extract workspace tarball in sandbox"); err != nil {
		return err
	}
	if verbose {
		fmt.Printf("Workspace location: %s\n", repoPath)
	}
	_, _ = execCommand(computer, SafeCommands.RmF(uploadTarPath), nil)
	return nil
}

func extractTarball(tarPath, dest string) error {
	file, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)

	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(hdr.Name)
		if strings.HasPrefix(name, "../") || strings.HasPrefix(name, "..\\") {
			return fmt.Errorf("tar entry outside destination: %s", hdr.Name)
		}
		target := filepath.Join(dest, name)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dest)) {
			return fmt.Errorf("tar entry outside destination: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil && !os.IsExist(err) {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(file, tarReader); err != nil {
				_ = file.Close()
				return err
			}
			_ = file.Close()
		default:
			// Skip unsupported types
		}
	}
	return nil
}

// DownloadWorkspace syncs workspace from sandbox to local.
func DownloadWorkspace(computer RemoteSandbox, opts SyncOptions, verbose bool) error {
	if verbose {
		fmt.Println("Creating tarball in sandbox...")
	}
	repoPath := resolveWorkspaceRepoPath(computer, opts.WorktreeID)
	resp, err := execCommand(computer, SafeCommands.TarCreate(downloadTarPath, repoPath), nil)
	if err != nil {
		return err
	}
	if err := assertCommandSuccess(resp, "failed to create tarball in sandbox"); err != nil {
		return err
	}
	remoteSize := getRemoteFileSize(computer, downloadTarPath)
	if verbose {
		fmt.Println("Tarball created in sandbox")
	}

	tmpFile, err := os.CreateTemp("", "amux-download-*.tgz")
	if err != nil {
		return err
	}
	_ = tmpFile.Close()
	localPath := tmpFile.Name()
	defer os.Remove(localPath)

	if verbose {
		fmt.Printf("Downloading to %s...\n", opts.Cwd)
	}
	if err := computer.DownloadFile(context.Background(), downloadTarPath, localPath); err != nil {
		return err
	}
	if verbose {
		fmt.Println("Download complete")
	}

	if remoteSize > 0 {
		if stat, err := os.Stat(localPath); err == nil {
			if stat.Size() != remoteSize {
				return fmt.Errorf("downloaded archive size mismatch (remote %d bytes, local %d bytes)", remoteSize, stat.Size())
			}
		}
	}

	retried := false
	retryWithBuffer := func(original error) error {
		if retried {
			return original
		}
		if remoteSize > maxBufferDownloadSize {
			return original
		}
		if verbose {
			fmt.Println("Retrying download using buffer...")
		}
		data, err := downloadBytes(context.Background(), computer, downloadTarPath)
		if err != nil {
			return original
		}
		if err := os.WriteFile(localPath, data, 0o644); err != nil {
			return err
		}
		retried = true
		return ensureGzipFile(localPath)
	}

	if err := ensureGzipFile(localPath); err != nil {
		if err := retryWithBuffer(err); err != nil {
			return err
		}
	}

	if verbose {
		fmt.Println("Extracting tarball locally...")
	}
	if err := extractTarball(localPath, opts.Cwd); err != nil {
		if !isArchiveError(err) {
			return err
		}
		if err := retryWithBuffer(err); err != nil {
			return err
		}
		if err := extractTarball(localPath, opts.Cwd); err != nil {
			return err
		}
	}

	_, _ = execCommand(computer, SafeCommands.RmF(downloadTarPath), nil)
	return nil
}

// GetWorktreeRepoPath returns the repo path inside sandbox.
func GetWorktreeRepoPath(computer RemoteSandbox, opts SyncOptions) string {
	return resolveWorkspaceRepoPath(computer, opts.WorktreeID)
}
