package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitTB is the subset of *testing.T the git fixtures need. Following the fataler
// pattern in wait.go, this lets the file avoid importing "testing" while still
// accepting a real *testing.T.
type gitTB interface {
	Helper()
	Fatalf(format string, args ...any)
	TempDir() string
}

// RunGit runs `git args...` in dir and returns its trimmed combined output,
// failing the test on error. It strips inherited GIT_DIR/GIT_WORK_TREE/
// GIT_INDEX_FILE (which a surrounding pre-push hook or harness may set) so the
// command always operates on dir, and pins a deterministic author/committer
// identity so commits succeed without depending on the machine's global git
// config.
func RunGit(t gitTB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	env := make([]string, 0, len(os.Environ())+4)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_DIR=") ||
			strings.HasPrefix(kv, "GIT_WORK_TREE=") ||
			strings.HasPrefix(kv, "GIT_INDEX_FILE=") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = append(env,
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// InitRepo creates a throwaway git repository under t.TempDir() with a single
// committed README on the default "main" branch and returns its path.
func InitRepo(t gitTB) string {
	return InitRepoWithBranch(t, "main")
}

// InitRepoWithBranch is InitRepo with a caller-chosen initial branch name.
func InitRepoWithBranch(t gitTB, branch string) string {
	t.Helper()
	root := t.TempDir()
	RunGit(t, root, "init", "-b", branch)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("init\n"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}
	RunGit(t, root, "add", "README.md")
	RunGit(t, root, "commit", "-m", "init")
	return root
}
