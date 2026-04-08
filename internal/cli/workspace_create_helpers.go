package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/git"
)

func canonicalizeProjectPath(projectPath string) (string, error) {
	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return "", err
	}
	canonicalPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", err
	}
	return canonicalPath, nil
}

func waitForPath(path string, attempts int, delay time.Duration) error {
	if attempts <= 0 {
		return fmt.Errorf("%s did not appear in time", path)
	}
	for i := 0; i < attempts; i++ {
		_, err := os.Stat(path)
		if err == nil {
			return nil
		}
		if !os.IsNotExist(err) {
			return err
		}
		time.Sleep(delay)
	}
	return fmt.Errorf("%s did not appear in time", path)
}

func resolveWorkspaceBaseFallback(projectPath, detected string, detectedErr error) string {
	if detectedErr != nil {
		return "HEAD"
	}

	base := strings.TrimSpace(detected)
	if base == "" {
		return "HEAD"
	}
	if gitRefExists(projectPath, base) {
		return base
	}

	remoteBase := "origin/" + base
	if gitRefExists(projectPath, remoteBase) {
		return remoteBase
	}
	return "HEAD"
}

func isProjectRegistered(registered []string, projectPath string) bool {
	for _, p := range registered {
		canon, err := canonicalizeProjectPath(p)
		if err != nil {
			continue
		}
		if canon == projectPath {
			return true
		}
	}
	return false
}

func gitRefExists(repoPath, ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	_, err := git.RunGitCtx(context.Background(), repoPath, "rev-parse", "--verify", ref)
	return err == nil
}

func gitLocalBranchExists(repoPath, branchName string) bool {
	branchName = strings.TrimSpace(branchName)
	if branchName == "" {
		return false
	}
	_, err := git.RunGitCtx(context.Background(), repoPath, "rev-parse", "--verify", "refs/heads/"+branchName)
	return err == nil
}
