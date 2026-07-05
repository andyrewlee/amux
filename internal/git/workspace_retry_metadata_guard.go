package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

var workspaceCleanupRetryFingerprintCtx = workspaceCleanupRetryFingerprintWithContext

func workspaceCleanupRetryFingerprint(workspacePath string) (string, error) {
	return workspaceCleanupRetryFingerprintCtx(context.Background(), workspacePath)
}

func workspaceCleanupRetryFingerprintWithContext(ctx context.Context, workspacePath string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(workspacePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()

	hasher := sha256.New()
	retryMetadataPath := workspaceCleanupRetryMetadataPath(workspacePath)
	retryMetadataRel, err := filepath.Rel(workspacePath, retryMetadataPath)
	if err != nil {
		return "", err
	}
	retryMetadataRel = filepath.ToSlash(retryMetadataRel)
	err = fs.WalkDir(root.FS(), ".", func(relPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if relPath == retryMetadataRel {
			return nil
		}
		if relPath == "." && d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(
			hasher,
			"%s|%s|%d|%d\n",
			relPath,
			info.Mode().String(),
			info.Size(),
			info.ModTime().UnixNano(),
		)
		if d.Type()&fs.ModeSymlink != 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			target, err := root.Readlink(filepath.FromSlash(relPath))
			if err != nil {
				return err
			}
			fmt.Fprintf(hasher, "symlink=%s\n", target)
		}
		if relPath == ".git" && !d.IsDir() {
			if err := ctx.Err(); err != nil {
				return err
			}
			content, err := root.ReadFile(filepath.FromSlash(relPath))
			if err != nil {
				return err
			}
			hasher.Write(content)
			hasher.Write([]byte{0})
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func workspacePathMatchesRetryMetadataCleanup(workspacePath string, metadata workspaceCleanupRetryMetadata) (bool, error) {
	return workspacePathMatchesRetryMetadataCleanupWithContext(context.Background(), workspacePath, metadata)
}

func workspacePathMatchesRetryMetadataCleanupWithContext(
	ctx context.Context,
	workspacePath string,
	metadata workspaceCleanupRetryMetadata,
) (bool, error) {
	if _, statErr := os.Stat(workspacePath); os.IsNotExist(statErr) {
		return false, nil
	} else if statErr != nil {
		return false, statErr
	}
	if metadata.WorkspaceFingerprint == "" {
		// Legacy retry metadata predated workspace fingerprints. Preserve
		// upgrade compatibility for those marker-only leftovers, but only
		// while the path still has no .git file so we don't prune a
		// recreated worktree.
		gitPath := filepath.Join(workspacePath, ".git")
		if _, err := os.Stat(gitPath); os.IsNotExist(err) {
			return true, nil
		} else if err != nil {
			return false, err
		}
		return false, nil
	}
	currentFingerprint, err := workspaceCleanupRetryFingerprintCtx(ctx, workspacePath)
	if err != nil {
		return false, err
	}
	return currentFingerprint == metadata.WorkspaceFingerprint, nil
}

func rejectReusedWorkspacePathForRetryMetadataCleanup(workspacePath string, metadata workspaceCleanupRetryMetadata) error {
	matches, err := workspacePathMatchesRetryMetadataCleanup(workspacePath, metadata)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("workspace path %s exists while pending cleanup remains", workspacePath)
	}
	return nil
}
