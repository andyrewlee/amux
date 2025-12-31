package git

import (
	"bytes"
	"os/exec"
	"strings"
)

// RunGit executes a git command in the specified directory
func RunGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// Include stderr in error for debugging
		if stderr.Len() > 0 {
			return "", &GitError{
				Command: strings.Join(args, " "),
				Stderr:  stderr.String(),
				Err:     err,
			}
		}
		return "", err
	}

	return strings.TrimSpace(stdout.String()), nil
}

// GitError wraps git command errors with additional context
type GitError struct {
	Command string
	Stderr  string
	Err     error
}

func (e *GitError) Error() string {
	return "git " + e.Command + ": " + e.Stderr
}

func (e *GitError) Unwrap() error {
	return e.Err
}

// IsGitRepository checks if the given path is a git repository
func IsGitRepository(path string) bool {
	_, err := RunGit(path, "rev-parse", "--git-dir")
	return err == nil
}

// GetRepoRoot returns the root directory of the git repository
func GetRepoRoot(path string) (string, error) {
	return RunGit(path, "rev-parse", "--show-toplevel")
}

// GetCurrentBranch returns the current branch name
func GetCurrentBranch(path string) (string, error) {
	return RunGit(path, "rev-parse", "--abbrev-ref", "HEAD")
}

// GetRemoteURL returns the URL of the specified remote
func GetRemoteURL(path, remote string) (string, error) {
	return RunGit(path, "remote", "get-url", remote)
}
