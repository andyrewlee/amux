package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// FileManifest represents a file in the workspace.
type FileManifest struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
	Hash    string `json:"hash,omitempty"` // SHA256 hash for content comparison
	IsDir   bool   `json:"is_dir"`
	Mode    uint32 `json:"mode"`
}

// WorkspaceManifest contains information about all files in a workspace.
type WorkspaceManifest struct {
	Version   int                      `json:"version"`
	Generated time.Time                `json:"generated"`
	RootPath  string                   `json:"root_path"`
	Files     map[string]*FileManifest `json:"files"`
	TotalSize int64                    `json:"total_size"`
}

// SyncDiff represents the differences between local and remote workspaces.
type SyncDiff struct {
	Added    []string // Files to upload
	Modified []string // Files to re-upload
	Deleted  []string // Files to delete remotely
	Stats    SyncStats
}

// SyncStats provides statistics about sync operations.
type SyncStats struct {
	FilesAdded     int
	FilesModified  int
	FilesDeleted   int
	FilesUnchanged int
	BytesToUpload  int64
	BytesToDelete  int64
}

const (
	manifestFileName = ".amux-manifest.json"
	maxHashFileSize  = 10 * 1024 * 1024 // 10MB - only hash files smaller than this
	manifestVersion  = 1
)

// compileIgnorePatterns converts glob-style patterns to regex matchers.
func compileIgnorePatterns(patterns []string) []*regexp.Regexp {
	matchers := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		// Convert glob pattern to regex
		regexPattern := globToRegex(pattern)
		if re, err := regexp.Compile(regexPattern); err == nil {
			matchers = append(matchers, re)
		}
	}
	return matchers
}

// globToRegex converts a simple glob pattern to a regex pattern.
func globToRegex(glob string) string {
	var result strings.Builder
	result.WriteString("^")
	for _, ch := range glob {
		switch ch {
		case '*':
			result.WriteString(".*")
		case '?':
			result.WriteString(".")
		case '.', '+', '^', '$', '(', ')', '[', ']', '{', '}', '|', '\\':
			result.WriteRune('\\')
			result.WriteRune(ch)
		default:
			result.WriteRune(ch)
		}
	}
	result.WriteString("$")
	return result.String()
}

// GenerateLocalManifest creates a manifest of the local workspace.
func GenerateLocalManifest(rootPath string, ignorePatterns []string) (*WorkspaceManifest, error) {
	manifest := &WorkspaceManifest{
		Version:   manifestVersion,
		Generated: time.Now(),
		RootPath:  rootPath,
		Files:     make(map[string]*FileManifest),
	}

	// Compile ignore patterns
	matchers := compileIgnorePatterns(ignorePatterns)

	err := filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		// Get relative path
		relPath, err := filepath.Rel(rootPath, path)
		if err != nil {
			return nil
		}

		// Skip root
		if relPath == "." {
			return nil
		}

		// Check ignore patterns
		for _, matcher := range matchers {
			if matcher.MatchString(relPath) || matcher.MatchString(filepath.Base(relPath)) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		fm := &FileManifest{
			Path:    relPath,
			Size:    info.Size(),
			ModTime: info.ModTime().UnixNano(),
			IsDir:   d.IsDir(),
			Mode:    uint32(info.Mode()),
		}

		// Compute hash for small files
		if !d.IsDir() && info.Size() > 0 && info.Size() < maxHashFileSize {
			if hash, err := hashFile(path); err == nil {
				fm.Hash = hash
			}
		}

		manifest.Files[relPath] = fm
		manifest.TotalSize += info.Size()

		return nil
	})

	return manifest, err
}

// GetRemoteManifest retrieves the manifest from the remote sandbox.
func GetRemoteManifest(computer RemoteSandbox, remotePath string) (*WorkspaceManifest, error) {
	manifestPath := filepath.Join(remotePath, manifestFileName)

	// Try to read the manifest file
	resp, err := execCommand(computer, SafeCommands.Cat(manifestPath), nil)
	if err != nil || resp.ExitCode != 0 {
		// No manifest exists - return empty
		return &WorkspaceManifest{
			Version: manifestVersion,
			Files:   make(map[string]*FileManifest),
		}, nil
	}

	content := getStdout(resp)
	if content == "" {
		return &WorkspaceManifest{
			Version: manifestVersion,
			Files:   make(map[string]*FileManifest),
		}, nil
	}

	var manifest WorkspaceManifest
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		// Corrupted manifest - return empty
		LogWarn("corrupted remote manifest, will do full sync", "error", err)
		return &WorkspaceManifest{
			Version: manifestVersion,
			Files:   make(map[string]*FileManifest),
		}, nil
	}

	return &manifest, nil
}

// uploadTimeout is the default timeout for file upload operations.
const uploadTimeout = 5 * time.Minute

// SaveRemoteManifest saves the manifest to the remote sandbox.
func SaveRemoteManifest(computer RemoteSandbox, remotePath string, manifest *WorkspaceManifest) error {
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), uploadTimeout)
	defer cancel()

	manifestPath := filepath.Join(remotePath, manifestFileName)
	return uploadBytes(ctx, computer, data, manifestPath)
}

// ComputeDiff calculates the differences between local and remote manifests.
func ComputeDiff(local, remote *WorkspaceManifest) *SyncDiff {
	diff := &SyncDiff{
		Added:    make([]string, 0),
		Modified: make([]string, 0),
		Deleted:  make([]string, 0),
	}

	// Find added and modified files
	for path, localFile := range local.Files {
		if localFile.IsDir {
			continue // Skip directories in diff
		}

		remoteFile, exists := remote.Files[path]
		if !exists {
			diff.Added = append(diff.Added, path)
			diff.Stats.FilesAdded++
			diff.Stats.BytesToUpload += localFile.Size
		} else if isFileModified(localFile, remoteFile) {
			diff.Modified = append(diff.Modified, path)
			diff.Stats.FilesModified++
			diff.Stats.BytesToUpload += localFile.Size
		} else {
			diff.Stats.FilesUnchanged++
		}
	}

	// Find deleted files
	for path, remoteFile := range remote.Files {
		if remoteFile.IsDir {
			continue
		}
		if _, exists := local.Files[path]; !exists {
			diff.Deleted = append(diff.Deleted, path)
			diff.Stats.FilesDeleted++
			diff.Stats.BytesToDelete += remoteFile.Size
		}
	}

	// Sort for deterministic ordering
	sort.Strings(diff.Added)
	sort.Strings(diff.Modified)
	sort.Strings(diff.Deleted)

	return diff
}

// isFileModified checks if a file has been modified.
func isFileModified(local, remote *FileManifest) bool {
	// If we have hashes, compare them (most accurate)
	if local.Hash != "" && remote.Hash != "" {
		return local.Hash != remote.Hash
	}

	// Fall back to size and modification time
	if local.Size != remote.Size {
		return true
	}

	// If mod time is significantly different (> 1 second), consider modified
	if local.ModTime > 0 && remote.ModTime > 0 {
		timeDiff := local.ModTime - remote.ModTime
		if timeDiff < 0 {
			timeDiff = -timeDiff
		}
		return timeDiff > int64(time.Second)
	}

	return false
}

// IncrementalSync performs an incremental sync of the workspace.
func IncrementalSync(computer RemoteSandbox, opts SyncOptions, verbose bool) error {
	logger := GetLogger().With("op", "incremental_sync")

	// Get ignore patterns
	patterns, err := getIgnorePatterns(opts)
	if err != nil {
		return ErrSyncFailed("get_ignore_patterns", err)
	}

	// Generate local manifest
	logger.Debug("generating local manifest")
	localManifest, err := GenerateLocalManifest(opts.Cwd, patterns)
	if err != nil {
		return ErrSyncFailed("generate_local_manifest", err)
	}

	remotePath := GetWorktreeRepoPath(computer, opts)

	// Get remote manifest
	logger.Debug("fetching remote manifest")
	remoteManifest, err := GetRemoteManifest(computer, remotePath)
	if err != nil {
		return ErrSyncFailed("get_remote_manifest", err)
	}

	// Compute diff
	diff := ComputeDiff(localManifest, remoteManifest)

	if verbose {
		fmt.Printf("Sync: %d added, %d modified, %d deleted, %d unchanged\n",
			diff.Stats.FilesAdded, diff.Stats.FilesModified,
			diff.Stats.FilesDeleted, diff.Stats.FilesUnchanged)
	}

	// If many files changed, fall back to full sync
	totalChanges := diff.Stats.FilesAdded + diff.Stats.FilesModified + diff.Stats.FilesDeleted
	totalFiles := totalChanges + diff.Stats.FilesUnchanged
	if totalFiles > 0 && float64(totalChanges)/float64(totalFiles) > 0.5 {
		logger.Debug("too many changes, falling back to full sync",
			"changes", totalChanges, "total", totalFiles)
		return UploadWorkspace(computer, opts, verbose)
	}

	// No changes
	if totalChanges == 0 {
		if verbose {
			fmt.Println("Workspace is up to date")
		}
		return nil
	}

	// Ensure remote directory exists
	_, _ = execCommand(computer, SafeCommands.MkdirP(remotePath), nil)

	// Delete removed files
	for _, path := range diff.Deleted {
		fullPath := filepath.Join(remotePath, path)
		logger.Debug("deleting", "path", path)
		_, _ = execCommand(computer, SafeCommands.RmRf(fullPath), nil)
	}

	// Upload added and modified files
	filesToUpload := append(diff.Added, diff.Modified...)
	for _, path := range filesToUpload {
		localPath := filepath.Join(opts.Cwd, path)
		remoteFull := filepath.Join(remotePath, path)

		// Ensure parent directory exists
		parentDir := filepath.Dir(remoteFull)
		_, _ = execCommand(computer, SafeCommands.MkdirP(parentDir), nil)

		// Read and upload file
		data, err := os.ReadFile(localPath)
		if err != nil {
			logger.Warn("failed to read file", "path", path, "error", err)
			continue
		}

		logger.Debug("uploading", "path", path, "size", len(data))
		uploadCtx, uploadCancel := context.WithTimeout(context.Background(), uploadTimeout)
		uploadErr := uploadBytes(uploadCtx, computer, data, remoteFull)
		uploadCancel()
		if uploadErr != nil {
			logger.Warn("failed to upload file", "path", path, "error", uploadErr)
			continue
		}
	}

	// Save updated manifest
	localManifest.Generated = time.Now()
	if err := SaveRemoteManifest(computer, remotePath, localManifest); err != nil {
		logger.Warn("failed to save manifest", "error", err)
	}

	if verbose {
		fmt.Printf("Synced %d files (%.2f KB)\n",
			len(filesToUpload),
			float64(diff.Stats.BytesToUpload)/1024)
	}

	return nil
}

// hashFile computes the SHA256 hash of a file.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// ShouldUseIncrementalSync determines if incremental sync is appropriate.
func ShouldUseIncrementalSync(computer RemoteSandbox, opts SyncOptions) bool {
	remotePath := GetWorktreeRepoPath(computer, opts)
	manifestPath := filepath.Join(remotePath, manifestFileName)

	// Check if manifest exists
	resp, err := execCommand(computer, SafeCommands.Test("-f", manifestPath), nil)
	if err != nil || resp.ExitCode != 0 {
		return false // No manifest, do full sync
	}

	// Check manifest age (if older than 7 days, do full sync for safety)
	cmd := SafeCommands.Stat(manifestPath)
	resp, err = execCommand(computer, cmd, nil)
	if err != nil || resp.ExitCode != 0 {
		return true // Can't get age, try incremental anyway
	}

	modTimeStr := strings.TrimSpace(getStdout(resp))
	var modTime int64
	_, _ = fmt.Sscanf(modTimeStr, "%d", &modTime)

	if modTime > 0 {
		age := time.Since(time.Unix(modTime, 0))
		if age > 7*24*time.Hour {
			LogDebug("manifest too old, will do full sync", "age", age)
			return false
		}
	}

	return true
}

// SmartSync chooses between incremental and full sync automatically.
func SmartSync(computer RemoteSandbox, opts SyncOptions, verbose bool) error {
	if ShouldUseIncrementalSync(computer, opts) {
		err := IncrementalSync(computer, opts, verbose)
		if err == nil {
			return nil
		}
		LogWarn("incremental sync failed, falling back to full sync", "error", err)
	}

	return UploadWorkspace(computer, opts, verbose)
}
