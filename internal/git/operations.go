package git

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
)

// RunGit executes a git command in the specified directory
func RunGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	// Filter out GIT_ environment variables to ensure we run against the target repo
	// and ignore any parent git process environment (e.g. when running in hooks)
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_DIR=") &&
			!strings.HasPrefix(e, "GIT_WORK_TREE=") &&
			!strings.HasPrefix(e, "GIT_INDEX_FILE=") {
			env = append(env, e)
		}
	}
	cmd.Env = env

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

// RunGitAllowFailure executes git and returns stdout even if exit code is non-zero.
// Use for commands like `git diff --no-index` which return 1 when differences exist.
func RunGitAllowFailure(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	// Filter out GIT_ environment variables
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_DIR=") &&
			!strings.HasPrefix(e, "GIT_WORK_TREE=") &&
			!strings.HasPrefix(e, "GIT_INDEX_FILE=") {
			env = append(env, e)
		}
	}
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	_ = cmd.Run() // Ignore exit code - some commands return 1 on success

	// Only return error if there's actual stderr output indicating a problem
	// and no stdout (which would indicate the command worked but returned non-zero)
	if stderr.Len() > 0 && stdout.Len() == 0 {
		return "", &GitError{
			Command: strings.Join(args, " "),
			Stderr:  stderr.String(),
			Err:     nil,
		}
	}

	return strings.TrimSpace(stdout.String()), nil
}

// RunGitRaw executes a git command and returns raw bytes without trimming.
// Use this for commands with -z output that use NUL separators.
func RunGitRaw(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	// Filter out GIT_ environment variables
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_DIR=") &&
			!strings.HasPrefix(e, "GIT_WORK_TREE=") &&
			!strings.HasPrefix(e, "GIT_INDEX_FILE=") {
			env = append(env, e)
		}
	}
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if stderr.Len() > 0 {
			return nil, &GitError{
				Command: strings.Join(args, " "),
				Stderr:  stderr.String(),
				Err:     err,
			}
		}
		return nil, err
	}

	return stdout.Bytes(), nil
}

// CreateCommit stages all changes and creates a commit with the given message.
// Returns the commit hash on success.
func CreateCommit(repoPath, message string) (string, error) {
	// Stage all changes
	if _, err := RunGit(repoPath, "add", "-A"); err != nil {
		return "", err
	}

	// Create commit
	if _, err := RunGit(repoPath, "commit", "-m", message); err != nil {
		return "", err
	}

	// Get the commit hash
	hash, err := RunGit(repoPath, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", err
	}

	return hash, nil
}

// MergeBranchToMain merges the given branch into the current branch (expected to be main/master).
// This should be run in the main repo directory.
func MergeBranchToMain(mainRepoPath, branch string) error {
	_, err := RunGit(mainRepoPath, "merge", branch)
	return err
}

// GetPRURL constructs a URL for creating a pull/merge request.
// Supports GitHub and GitLab.
func GetPRURL(repoPath, branch string) (string, error) {
	remoteURL, err := GetRemoteURL(repoPath, "origin")
	if err != nil {
		return "", err
	}

	// Normalize the URL to extract host and repo path
	url := normalizeGitURL(remoteURL)
	if url == "" {
		return "", &GitError{
			Command: "get PR URL",
			Stderr:  "unsupported remote URL format: " + remoteURL,
		}
	}

	// Determine if GitHub or GitLab based on URL
	if strings.Contains(url, "github.com") {
		// GitHub: https://github.com/owner/repo/compare/branch?expand=1
		return url + "/compare/" + branch + "?expand=1", nil
	} else if strings.Contains(url, "gitlab") {
		// GitLab: https://gitlab.com/owner/repo/-/merge_requests/new?merge_request[source_branch]=branch
		return url + "/-/merge_requests/new?merge_request[source_branch]=" + branch, nil
	}

	// Default to GitHub-style for unknown hosts
	return url + "/compare/" + branch + "?expand=1", nil
}

// normalizeGitURL converts git@ or https:// URLs to a web URL format.
func normalizeGitURL(remoteURL string) string {
	// Handle SSH URLs: git@github.com:owner/repo.git
	if strings.HasPrefix(remoteURL, "git@") {
		// Remove git@ prefix and .git suffix
		url := strings.TrimPrefix(remoteURL, "git@")
		url = strings.TrimSuffix(url, ".git")
		// Replace : with /
		url = strings.Replace(url, ":", "/", 1)
		return "https://" + url
	}

	// Handle HTTPS URLs: https://github.com/owner/repo.git
	if strings.HasPrefix(remoteURL, "https://") || strings.HasPrefix(remoteURL, "http://") {
		url := strings.TrimSuffix(remoteURL, ".git")
		return url
	}

	return ""
}
