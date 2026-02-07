package git

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// skipIfNoGit skips the test if git is not installed
func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	// Filter out GIT_ environment variables that might be set by pre-push hooks
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_DIR=") &&
			!strings.HasPrefix(e, "GIT_WORK_TREE=") &&
			!strings.HasPrefix(e, "GIT_INDEX_FILE=") {
			env = append(env, e)
		}
	}

	cmd.Env = append(env,
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func initRepo(t *testing.T) string {
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	if err := os.WriteFile(root+"/README.md", []byte("init"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "init")
	return root
}
