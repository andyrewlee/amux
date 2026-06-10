package git

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	writeRetryMarkerRenamePath = os.Rename
	writeRetryMarkerRemovePath = os.Remove
)

const prunedWorkspaceCleanupRetryMetadataName = ".amux-pruned-worktree-retry"

type workspaceCleanupState struct {
	RepoPath               string
	CleanupPath            string
	NeedsUnregister        bool
	WorkspaceGitRef        string
	WorkspaceGitRefModTime int64
	WorkspaceFingerprint   string
	LegacyAmbiguous        bool
}

type workspaceCleanupRetryMetadata struct {
	RepoPath             string
	NeedsUnregister      bool
	WorkspaceFingerprint string
}

func ensurePrunedWorkspaceRetryMarker(workspacePath, cleanupPath string) error {
	return ensurePrunedWorkspaceRetryMarkerWithState(workspacePath, cleanupPath, false)
}

func ensurePrunedWorkspaceRetryMarkerWithState(workspacePath, cleanupPath string, unregisterPending bool) error {
	state := workspaceCleanupState{
		NeedsUnregister: unregisterPending,
	}
	if cleanupPath != "" {
		state.CleanupPath = filepath.Clean(cleanupPath)
	}
	return writeWorkspaceCleanupState(workspacePath, state)
}

func readWorkspaceCleanupState(workspacePath string) (workspaceCleanupState, bool, error) {
	markerPath := prunedWorkspaceRetryMarkerPath(workspacePath)
	content, err := readWorkspaceCleanupMarkerFile(markerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return workspaceCleanupState{}, false, nil
		}
		return workspaceCleanupState{}, false, err
	}

	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		return workspaceCleanupState{}, false, fmt.Errorf("empty workspace cleanup marker: %s", markerPath)
	}
	// Current format: versioned JSON. Legacy formats (ambiguous prose,
	// single-line prefixed paths, key=value) remain readable below but are
	// never written anymore.
	if strings.HasPrefix(trimmed, "{") {
		state, err := parseWorkspaceCleanupMarkerJSON(trimmed, markerPath)
		if err != nil {
			return workspaceCleanupState{}, false, err
		}
		return state, true, nil
	}
	if trimmed == "pruned workspace cleanup pending" {
		return workspaceCleanupState{LegacyAmbiguous: true}, true, nil
	}

	// Backward-compatible single-line parse for existing plain-path markers.
	if !strings.Contains(trimmed, "=") {
		state := workspaceCleanupState{}
		switch {
		case strings.HasPrefix(trimmed, "su:"):
			state.NeedsUnregister = true
			state.CleanupPath = filepath.Clean(strings.TrimPrefix(trimmed, "su:"))
		case strings.HasPrefix(trimmed, "u:"):
			state.NeedsUnregister = true
			state.CleanupPath = filepath.Clean(strings.TrimPrefix(trimmed, "u:"))
		case strings.HasPrefix(trimmed, "s:"):
			state.CleanupPath = filepath.Clean(strings.TrimPrefix(trimmed, "s:"))
		default:
			state.CleanupPath = filepath.Clean(trimmed)
		}
		if state.CleanupPath == filepath.Clean(workspacePath) {
			state.LegacyAmbiguous = true
		}
		return state, true, nil
	}

	state := workspaceCleanupState{}
	var sawRepoPath bool
	var sawCleanupPath bool
	var sawNeedsUnregister bool
	var sawWorkspaceGitRef bool
	var sawWorkspaceGitRefModTime bool
	var sawWorkspaceFingerprint bool
	for _, line := range strings.Split(trimmed, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			return workspaceCleanupState{}, false, fmt.Errorf("invalid workspace cleanup marker line %q", line)
		}
		switch key {
		case "repo_path":
			if sawRepoPath {
				return workspaceCleanupState{}, false, fmt.Errorf("duplicate workspace cleanup marker field %q", key)
			}
			sawRepoPath = true
			if value != "" {
				state.RepoPath = filepath.Clean(value)
			}
		case "cleanup_path":
			if sawCleanupPath {
				return workspaceCleanupState{}, false, fmt.Errorf("duplicate workspace cleanup marker field %q", key)
			}
			sawCleanupPath = true
			if value != "" {
				state.CleanupPath = filepath.Clean(value)
			}
		case "needs_unregister":
			if sawNeedsUnregister {
				return workspaceCleanupState{}, false, fmt.Errorf("duplicate workspace cleanup marker field %q", key)
			}
			sawNeedsUnregister = true
			switch value {
			case "true":
				state.NeedsUnregister = true
			case "false":
				state.NeedsUnregister = false
			default:
				return workspaceCleanupState{}, false, fmt.Errorf("invalid needs_unregister value %q", value)
			}
		case "workspace_git_ref":
			if sawWorkspaceGitRef {
				return workspaceCleanupState{}, false, fmt.Errorf("duplicate workspace cleanup marker field %q", key)
			}
			sawWorkspaceGitRef = true
			state.WorkspaceGitRef = value
		case "workspace_git_ref_mtime_unix_nano":
			if sawWorkspaceGitRefModTime {
				return workspaceCleanupState{}, false, fmt.Errorf("duplicate workspace cleanup marker field %q", key)
			}
			sawWorkspaceGitRefModTime = true
			if value == "" {
				state.WorkspaceGitRefModTime = 0
				break
			}
			var parsed int64
			if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
				return workspaceCleanupState{}, false, fmt.Errorf("invalid workspace_git_ref_mtime_unix_nano value %q", value)
			}
			state.WorkspaceGitRefModTime = parsed
		case "workspace_fingerprint":
			if sawWorkspaceFingerprint {
				return workspaceCleanupState{}, false, fmt.Errorf("duplicate workspace cleanup marker field %q", key)
			}
			sawWorkspaceFingerprint = true
			state.WorkspaceFingerprint = value
		default:
			return workspaceCleanupState{}, false, fmt.Errorf("unknown workspace cleanup marker field %q", key)
		}
	}
	if !sawCleanupPath || !sawNeedsUnregister {
		return workspaceCleanupState{}, false, fmt.Errorf("incomplete workspace cleanup marker: %s", markerPath)
	}

	return state, true, nil
}

func writeWorkspaceCleanupState(workspacePath string, state workspaceCleanupState) error {
	if state.LegacyAmbiguous {
		return fmt.Errorf("refusing to persist legacy-ambiguous workspace cleanup state for %s", workspacePath)
	}
	if state.RepoPath != "" {
		state.RepoPath = filepath.Clean(state.RepoPath)
	}
	if state.CleanupPath != "" && !isSafeWorkspaceCleanupPath(state.CleanupPath) {
		return fmt.Errorf("refusing to persist unsafe workspace cleanup path: %s", state.CleanupPath)
	}
	if !state.NeedsUnregister && state.CleanupPath == "" {
		return clearPrunedWorkspaceRetryMarker(workspacePath)
	}

	payload, err := json.Marshal(workspaceCleanupMarkerFile{
		Version:                workspaceCleanupMarkerVersion,
		RepoPath:               state.RepoPath,
		CleanupPath:            state.CleanupPath,
		NeedsUnregister:        state.NeedsUnregister,
		WorkspaceGitRef:        state.WorkspaceGitRef,
		WorkspaceGitRefModTime: state.WorkspaceGitRefModTime,
		WorkspaceFingerprint:   state.WorkspaceFingerprint,
	})
	if err != nil {
		return fmt.Errorf("encode workspace cleanup marker: %w", err)
	}
	return writeRetryMarkerFile(prunedWorkspaceRetryMarkerPath(workspacePath), payload, 0o600)
}

// workspaceCleanupMarkerVersion is the current on-disk marker format version.
const workspaceCleanupMarkerVersion = 1

// workspaceCleanupMarkerFile is the versioned JSON codec for the cleanup
// marker. Read-side support for the legacy formats (prose, prefixed
// single-line, key=value) lives in readWorkspaceCleanupState; only this
// format is written.
type workspaceCleanupMarkerFile struct {
	Version                int    `json:"version"`
	RepoPath               string `json:"repo_path,omitempty"`
	CleanupPath            string `json:"cleanup_path,omitempty"`
	NeedsUnregister        bool   `json:"needs_unregister"`
	WorkspaceGitRef        string `json:"workspace_git_ref,omitempty"`
	WorkspaceGitRefModTime int64  `json:"workspace_git_ref_mtime_unix_nano,omitempty"`
	WorkspaceFingerprint   string `json:"workspace_fingerprint,omitempty"`
}

func parseWorkspaceCleanupMarkerJSON(trimmed, markerPath string) (workspaceCleanupState, error) {
	var file workspaceCleanupMarkerFile
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return workspaceCleanupState{}, fmt.Errorf("invalid workspace cleanup marker %s: %w", markerPath, err)
	}
	if file.Version != workspaceCleanupMarkerVersion {
		return workspaceCleanupState{}, fmt.Errorf("unsupported workspace cleanup marker version %d in %s", file.Version, markerPath)
	}
	state := workspaceCleanupState{
		NeedsUnregister:        file.NeedsUnregister,
		WorkspaceGitRef:        file.WorkspaceGitRef,
		WorkspaceGitRefModTime: file.WorkspaceGitRefModTime,
		WorkspaceFingerprint:   file.WorkspaceFingerprint,
	}
	if file.RepoPath != "" {
		state.RepoPath = filepath.Clean(file.RepoPath)
	}
	if file.CleanupPath != "" {
		state.CleanupPath = filepath.Clean(file.CleanupPath)
	}
	return state, nil
}

func writeRetryMarkerFileAtomically(path string, payload []byte, perm os.FileMode) error {
	return writeRetryMarkerFileAtomicallyForGOOS(runtime.GOOS, path, payload, perm)
}

func writeRetryMarkerFileAtomicallyForGOOS(goos, path string, payload []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tempFile, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()

	if err := tempFile.Chmod(perm); err != nil {
		_ = tempFile.Close()
		return err
	}
	if _, err := tempFile.Write(payload); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if goos == "windows" {
		if err := replaceFileForWindows(path, tempPath); err != nil {
			return err
		}
	} else {
		if err := writeRetryMarkerRenamePath(tempPath, path); err != nil {
			return err
		}
	}
	cleanup = false
	return nil
}

func replaceFileForWindows(path, tempPath string) error {
	backupPath := retryMarkerBackupPath(path)
	hadPrimary := false
	hadBackupOnly := false
	if _, err := os.Stat(path); err == nil {
		hadPrimary = true
		if err := writeRetryMarkerRemovePath(backupPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := writeRetryMarkerRenamePath(path, backupPath); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	} else if _, err := os.Stat(backupPath); err == nil {
		hadBackupOnly = true
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := writeRetryMarkerRenamePath(tempPath, path); err != nil {
		if hadPrimary {
			_ = writeRetryMarkerRenamePath(backupPath, path)
		}
		return err
	}
	if hadPrimary || hadBackupOnly {
		_ = writeRetryMarkerRemovePath(backupPath)
	}
	return nil
}

func readWorkspaceCleanupMarkerFile(markerPath string) ([]byte, error) {
	content, err := os.ReadFile(markerPath)
	if err == nil {
		return content, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	return os.ReadFile(retryMarkerBackupPath(markerPath))
}

func clearPrunedWorkspaceRetryMarker(workspacePath string) error {
	markerPath := prunedWorkspaceRetryMarkerPath(workspacePath)
	if err := removeRetryMetadataPath(markerPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := removeRetryMetadataPath(retryMarkerBackupPath(markerPath)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func workspaceCleanupRetryMetadataPath(workspacePath string) string {
	return filepath.Join(workspacePath, prunedWorkspaceCleanupRetryMetadataName)
}

func ensureWorkspaceCleanupRetryMetadata(workspacePath, repoPath string, needsUnregister bool) error {
	_, err := ensureWorkspaceCleanupRetryMetadataWithContext(context.Background(), workspacePath, repoPath, needsUnregister)
	return err
}

func ensureWorkspaceCleanupRetryMetadataWithContext(
	ctx context.Context,
	workspacePath, repoPath string,
	needsUnregister bool,
) (workspaceCleanupRetryMetadata, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return workspaceCleanupRetryMetadata{}, err
	}
	if _, err := os.Stat(workspacePath); os.IsNotExist(err) {
		return workspaceCleanupRetryMetadata{}, nil
	} else if err != nil {
		return workspaceCleanupRetryMetadata{}, err
	}
	metadata := workspaceCleanupRetryMetadata{
		NeedsUnregister: needsUnregister,
	}
	if repoPath != "" {
		metadata.RepoPath = filepath.Clean(repoPath)
	}
	if metadata.NeedsUnregister && metadata.RepoPath == "" {
		return workspaceCleanupRetryMetadata{}, fmt.Errorf("workspace cleanup retry metadata for %s is missing repo path for unregister recovery", workspacePath)
	}
	fingerprint, err := workspaceCleanupRetryFingerprintCtx(ctx, workspacePath)
	if err != nil {
		return workspaceCleanupRetryMetadata{}, err
	}
	metadata.WorkspaceFingerprint = fingerprint
	if err := ctx.Err(); err != nil {
		return workspaceCleanupRetryMetadata{}, err
	}
	if err := os.WriteFile(
		workspaceCleanupRetryMetadataPath(workspacePath),
		[]byte(fmt.Sprintf(
			"repo_path=%s\nneeds_unregister=%t\nworkspace_fingerprint=%s\n",
			metadata.RepoPath,
			metadata.NeedsUnregister,
			metadata.WorkspaceFingerprint,
		)),
		0o600,
	); err != nil {
		return workspaceCleanupRetryMetadata{}, err
	}
	return metadata, nil
}

func readWorkspaceCleanupRetryMetadata(workspacePath string) (workspaceCleanupRetryMetadata, bool, error) {
	content, err := os.ReadFile(workspaceCleanupRetryMetadataPath(workspacePath))
	if err == nil {
		trimmed := strings.TrimSpace(string(content))
		switch trimmed {
		case "":
			return workspaceCleanupRetryMetadata{}, false, fmt.Errorf(
				"empty workspace cleanup retry metadata for %s",
				workspacePath,
			)
		case "pending workspace cleanup", "needs_unregister=false":
			return workspaceCleanupRetryMetadata{}, true, nil
		case "needs_unregister=true":
			return workspaceCleanupRetryMetadata{}, false, fmt.Errorf(
				"workspace cleanup retry metadata for %s is missing repo path for unregister recovery",
				workspacePath,
			)
		default:
			if !strings.Contains(trimmed, "=") {
				return workspaceCleanupRetryMetadata{}, false, fmt.Errorf(
					"invalid workspace cleanup retry metadata for %s",
					workspacePath,
				)
			}
		}
		metadata := workspaceCleanupRetryMetadata{}
		var sawRepoPath bool
		var sawNeedsUnregister bool
		var sawWorkspaceFingerprint bool
		for _, line := range strings.Split(trimmed, "\n") {
			key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
			if !ok {
				return workspaceCleanupRetryMetadata{}, false, fmt.Errorf(
					"invalid workspace cleanup retry metadata line %q",
					line,
				)
			}
			switch key {
			case "repo_path":
				if sawRepoPath {
					return workspaceCleanupRetryMetadata{}, false, fmt.Errorf(
						"duplicate workspace cleanup retry metadata field %q",
						key,
					)
				}
				sawRepoPath = true
				if value != "" {
					metadata.RepoPath = filepath.Clean(value)
				}
			case "needs_unregister":
				if sawNeedsUnregister {
					return workspaceCleanupRetryMetadata{}, false, fmt.Errorf(
						"duplicate workspace cleanup retry metadata field %q",
						key,
					)
				}
				sawNeedsUnregister = true
				switch value {
				case "true":
					metadata.NeedsUnregister = true
				case "false":
					metadata.NeedsUnregister = false
				default:
					return workspaceCleanupRetryMetadata{}, false, fmt.Errorf(
						"invalid workspace cleanup retry metadata needs_unregister value %q",
						value,
					)
				}
			case "workspace_fingerprint":
				if sawWorkspaceFingerprint {
					return workspaceCleanupRetryMetadata{}, false, fmt.Errorf(
						"duplicate workspace cleanup retry metadata field %q",
						key,
					)
				}
				sawWorkspaceFingerprint = true
				metadata.WorkspaceFingerprint = value
			default:
				return workspaceCleanupRetryMetadata{}, false, fmt.Errorf(
					"unknown workspace cleanup retry metadata field %q",
					key,
				)
			}
		}
		if !sawNeedsUnregister {
			return workspaceCleanupRetryMetadata{}, false, fmt.Errorf(
				"incomplete workspace cleanup retry metadata for %s",
				workspacePath,
			)
		}
		if metadata.NeedsUnregister && metadata.RepoPath == "" {
			return workspaceCleanupRetryMetadata{}, false, fmt.Errorf(
				"workspace cleanup retry metadata for %s is missing repo path for unregister recovery",
				workspacePath,
			)
		}
		return metadata, true, nil
	}
	if os.IsNotExist(err) {
		return workspaceCleanupRetryMetadata{}, false, nil
	}
	return workspaceCleanupRetryMetadata{}, false, err
}

func hasPendingWorkspaceCleanup(workspacePath string) (bool, error) {
	_, marked, err := readWorkspaceCleanupState(workspacePath)
	return marked, err
}

func prunedWorkspaceRetryMarkerPath(workspacePath string) string {
	dir := filepath.Dir(workspacePath)
	base := filepath.Base(workspacePath)
	return filepath.Join(dir, "."+base+prunedWorkspaceCleanupMarkerSuffix)
}

func retryMarkerBackupPath(path string) string {
	return path + ".bak"
}
