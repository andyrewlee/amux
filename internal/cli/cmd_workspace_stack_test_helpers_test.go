package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

type createdWorkspace struct {
	ID         string
	Name       string
	Branch     string
	Base       string
	BaseCommit string
	Root       string
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func initRegisteredRepo(t *testing.T, home string) string {
	t.Helper()

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(repoRoot) error = %v", err)
	}
	runGit(t, repoRoot, "init", "-b", "main")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "amux-test")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	runGit(t, repoRoot, "add", "README.md")
	runGit(t, repoRoot, "commit", "-m", "init")
	registerProject(t, home, repoRoot)
	return repoRoot
}

func initRegisteredRepoWithOrigin(t *testing.T, home string) (string, string) {
	t.Helper()

	repoRoot := initRegisteredRepo(t, home)
	remoteDir := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, t.TempDir(), "init", "--bare", remoteDir)
	remoteURL := "git@github.com:amux-test/origin.git"
	configureGitURLRewrite(t, repoRoot, remoteURL, remoteDir)
	runGit(t, repoRoot, "remote", "add", "origin", remoteURL)
	runGit(t, repoRoot, "push", "-u", "origin", "main")
	return repoRoot, remoteDir
}

func configureGitURLRewrite(t *testing.T, repoRoot, publicURL, targetPath string) {
	t.Helper()
	rewriteURL := "file://" + targetPath
	runGit(t, repoRoot, "config", "--local", "url."+rewriteURL+".insteadOf", publicURL)
}

func createWorkspaceForTest(t *testing.T, version string, args ...string) createdWorkspace {
	t.Helper()

	fullArgs := append([]string{}, args...)
	var out, errOut bytes.Buffer
	code := cmdWorkspaceCreate(&out, &errOut, GlobalFlags{JSON: true}, fullArgs, version)
	if code != ExitOK {
		t.Fatalf("cmdWorkspaceCreate() code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}
	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
	}
	payload, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected create payload object, got %T", env.Data)
	}
	return createdWorkspace{
		ID:         stringValue(payload["id"]),
		Name:       stringValue(payload["name"]),
		Branch:     stringValue(payload["branch"]),
		Base:       stringValue(payload["base"]),
		BaseCommit: stringValue(payload["base_commit"]),
		Root:       stringValue(payload["root"]),
	}
}

func loadWorkspaceFromHome(t *testing.T, home, workspaceID string) *data.Workspace {
	t.Helper()
	store := data.NewWorkspaceStore(filepath.Join(home, ".amux", "workspaces-metadata"))
	ws, err := store.Load(data.WorkspaceID(workspaceID))
	if err != nil {
		t.Fatalf("store.Load(%s) error = %v", workspaceID, err)
	}
	return ws
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
