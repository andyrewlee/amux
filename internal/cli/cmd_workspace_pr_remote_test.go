package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestCmdWorkspacePRHonorsRemoteForGHOperations(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot, _ := initRegisteredRepoWithOrigin(t, home)

	upstreamDir := filepath.Join(t.TempDir(), "upstream.git")
	forkDir := filepath.Join(t.TempDir(), "fork.git")
	runGit(t, t.TempDir(), "init", "--bare", upstreamDir)
	runGit(t, t.TempDir(), "init", "--bare", forkDir)
	upstreamFetchURL := "git@github.com:amux-test/upstream.git"
	forkPushURL := "git@github.com:fork-user/upstream.git"
	configureGitURLRewrite(t, repoRoot, upstreamFetchURL, upstreamDir)
	configureGitURLRewrite(t, repoRoot, forkPushURL, forkDir)
	runGit(t, repoRoot, "remote", "add", "upstream", upstreamFetchURL)
	runGit(t, repoRoot, "push", "-u", "upstream", "main")
	runGit(t, repoRoot, "remote", "set-url", "--push", "upstream", forkPushURL)

	workspace := createWorkspaceForTest(
		t,
		"test-v1",
		"feature", "--project", repoRoot, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(workspace.Root, "feature.txt"), []byte("feature-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(feature-1) error = %v", err)
	}
	runGit(t, workspace.Root, "add", "feature.txt")
	runGit(t, workspace.Root, "commit", "-m", "feature 1")

	storePath, restore := stubGHCLIForTest(t)
	defer restore()
	saveFakePRStore(t, storePath, fakePRStore{
		Next: 41,
		PRs: map[string]ghPRView{
			"other-user:feature": {
				Number:              41,
				URL:                 "https://example.com/pr/41",
				Title:               "other fork feature",
				HeadRefName:         "feature",
				HeadRepositoryOwner: ghPRRepositoryOwner{Login: "other-user"},
				BaseRefName:         "main",
				State:               "OPEN",
			},
		},
	})

	var out, errOut bytes.Buffer
	code := cmdWorkspacePR(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{workspace.ID, "--remote", "upstream"},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdWorkspacePR(--remote upstream) code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	out.Reset()
	errOut.Reset()
	code = cmdWorkspacePR(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{workspace.ID, "--remote", "upstream"},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("second cmdWorkspacePR(--remote upstream) code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	store := loadFakePRStore(t, storePath)
	if len(store.Calls) == 0 {
		t.Fatal("expected gh calls to be recorded")
	}
	sawForkHead := false
	sawBranchOnlyLookup := false
	createCalls := 0
	for _, call := range store.Calls {
		if call.Repo != "amux-test/upstream" {
			t.Fatalf("gh call repo = %q, want %q", call.Repo, "amux-test/upstream")
		}
		if (call.Subcommand == "create" || call.Subcommand == "view") && call.Head == "fork-user:feature" {
			sawForkHead = true
		}
		if call.Subcommand == "list" && call.Head == "feature" {
			sawBranchOnlyLookup = true
		}
		if call.Subcommand == "create" {
			createCalls++
		}
	}
	if !sawForkHead {
		t.Fatalf("expected gh to use fork head selector %q, calls = %#v", "fork-user:feature", store.Calls)
	}
	if !sawBranchOnlyLookup {
		t.Fatalf("expected gh list lookups to use branch-only head %q, calls = %#v", "feature", store.Calls)
	}
	if createCalls != 1 {
		t.Fatalf("create calls = %d, want %d; calls = %#v", createCalls, 1, store.Calls)
	}
	if _, ok := store.PRs["fork-user:feature"]; !ok {
		t.Fatalf("expected fork-user PR to be created, store = %#v", store.PRs)
	}
}

func TestCmdWorkspacePRRejectsBrokenStackParent(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot, _ := initRegisteredRepoWithOrigin(t, home)

	parent := createWorkspaceForTest(
		t,
		"test-v1",
		"feature", "--project", repoRoot, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(parent.Root, "parent.txt"), []byte("parent-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(parent-1) error = %v", err)
	}
	runGit(t, parent.Root, "add", "parent.txt")
	runGit(t, parent.Root, "commit", "-m", "parent 1")

	child := createWorkspaceForTest(
		t,
		"test-v1",
		"refactor", "--from-workspace", parent.ID, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(child.Root, "child.txt"), []byte("child-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(child-1) error = %v", err)
	}
	runGit(t, child.Root, "add", "child.txt")
	runGit(t, child.Root, "commit", "-m", "child 1")

	store := data.NewWorkspaceStore(filepath.Join(home, ".amux", "workspaces-metadata"))
	if err := store.Delete(data.WorkspaceID(parent.ID)); err != nil {
		t.Fatalf("store.Delete(%s) error = %v", parent.ID, err)
	}

	storePath, restore := stubGHCLIForTest(t)
	defer restore()

	var out, errOut bytes.Buffer
	code := cmdWorkspacePR(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{child.ID},
		"test-v1",
	)
	if code != ExitUnsafeBlocked {
		t.Fatalf("cmdWorkspacePR(broken stack) code = %d, want %d; stderr: %s; stdout: %s", code, ExitUnsafeBlocked, errOut.String(), out.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal(error) error = %v\nraw: %s", err, out.String())
	}
	if env.OK || env.Error == nil {
		t.Fatalf("expected error envelope, got %#v", env)
	}
	if env.Error.Code != "broken_stack" {
		t.Fatalf("error code = %q, want %q", env.Error.Code, "broken_stack")
	}
	if !strings.Contains(env.Error.Message, "missing or archived") {
		t.Fatalf("error message = %q, want mention of missing parent", env.Error.Message)
	}

	prStore := loadFakePRStore(t, storePath)
	if len(prStore.Calls) != 0 {
		t.Fatalf("expected no gh calls when stack is broken, got %#v", prStore.Calls)
	}
}

func TestCmdWorkspacePRNoAheadDoesNotPushRemoteBranch(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot, remoteDir := initRegisteredRepoWithOrigin(t, home)

	workspace := createWorkspaceForTest(
		t,
		"test-v1",
		"feature", "--project", repoRoot, "--assistant", "claude",
	)

	storePath, restore := stubGHCLIForTest(t)
	defer restore()

	var out, errOut bytes.Buffer
	code := cmdWorkspacePR(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{workspace.ID},
		"test-v1",
	)
	if code != ExitInternalError {
		t.Fatalf("cmdWorkspacePR(no ahead) code = %d, want %d; stderr: %s; stdout: %s", code, ExitInternalError, errOut.String(), out.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal(error) error = %v\nraw: %s", err, out.String())
	}
	if env.OK || env.Error == nil {
		t.Fatalf("expected error envelope, got %#v", env)
	}
	if env.Error.Code != "pr_failed" {
		t.Fatalf("error code = %q, want %q", env.Error.Code, "pr_failed")
	}
	if !strings.Contains(env.Error.Message, "no commits ahead") {
		t.Fatalf("error message = %q, want no-commits-ahead detail", env.Error.Message)
	}

	if remoteHasBranch(t, remoteDir, workspace.Branch) {
		t.Fatalf("unexpected remote branch push for empty workspace branch %q", workspace.Branch)
	}

	prStore := loadFakePRStore(t, storePath)
	if _, ok := prStore.PRs[workspace.Branch]; ok {
		t.Fatalf("unexpected PR created for empty workspace branch %q", workspace.Branch)
	}
}

func TestCmdWorkspacePRRecursiveForkChildrenTargetForkRepo(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot, _ := initRegisteredRepoWithOrigin(t, home)

	upstreamDir := filepath.Join(t.TempDir(), "upstream.git")
	forkDir := filepath.Join(t.TempDir(), "fork.git")
	runGit(t, t.TempDir(), "init", "--bare", upstreamDir)
	runGit(t, t.TempDir(), "init", "--bare", forkDir)
	upstreamFetchURL := "git@github.com:amux-test/upstream.git"
	forkPushURL := "git@github.com:fork-user/upstream.git"
	configureGitURLRewrite(t, repoRoot, upstreamFetchURL, upstreamDir)
	configureGitURLRewrite(t, repoRoot, forkPushURL, forkDir)
	runGit(t, repoRoot, "remote", "add", "upstream", upstreamFetchURL)
	runGit(t, repoRoot, "push", "-u", "upstream", "main")
	runGit(t, repoRoot, "remote", "set-url", "--push", "upstream", forkPushURL)

	root := createWorkspaceForTest(
		t,
		"test-v1",
		"feature", "--project", repoRoot, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(root.Root, "root.txt"), []byte("root-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(root-1) error = %v", err)
	}
	runGit(t, root.Root, "add", "root.txt")
	runGit(t, root.Root, "commit", "-m", "root 1")

	child := createWorkspaceForTest(
		t,
		"test-v1",
		"refactor", "--from-workspace", root.ID, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(child.Root, "child.txt"), []byte("child-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(child-1) error = %v", err)
	}
	runGit(t, child.Root, "add", "child.txt")
	runGit(t, child.Root, "commit", "-m", "child 1")

	storePath, restore := stubGHCLIForTest(t)
	defer restore()

	var out, errOut bytes.Buffer
	code := cmdWorkspacePR(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{root.ID, "--recursive", "--remote", "upstream"},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdWorkspacePR(recursive fork) code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	store := loadFakePRStore(t, storePath)
	sawRootCreate := false
	sawChildCreate := false
	for _, call := range store.Calls {
		if call.Subcommand != "create" {
			continue
		}
		if call.Head == "fork-user:feature" && call.Repo == "amux-test/upstream" {
			sawRootCreate = true
		}
		if call.Head == "fork-user:feature.refactor" && call.Repo == "fork-user/upstream" {
			sawChildCreate = true
		}
	}
	if !sawRootCreate {
		t.Fatalf("expected root PR create against upstream repo, calls = %#v", store.Calls)
	}
	if !sawChildCreate {
		t.Fatalf("expected child PR create against fork repo, calls = %#v", store.Calls)
	}
}

func TestGHPRRemoteConfigFromRemoteURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fetchURL string
		pushURL  string
		wantBase string
		wantHead string
		wantErr  bool
	}{
		{
			name:     "same repo",
			fetchURL: "https://github.com/openai/amux.git",
			pushURL:  "git@github.com:openai/amux.git",
			wantBase: "openai/amux",
			wantHead: "",
		},
		{
			name:     "fork push url",
			fetchURL: "git@github.com:upstream/amux.git",
			pushURL:  "git@github.com:fork-user/amux.git",
			wantBase: "upstream/amux",
			wantHead: "fork-user",
		},
		{
			name:     "enterprise ssh",
			fetchURL: "ssh://git@github.example.com/openai/amux.git",
			pushURL:  "ssh://git@github.example.com/forker/amux.git",
			wantBase: "github.example.com/openai/amux",
			wantHead: "forker",
		},
		{name: "local path", fetchURL: "/tmp/remote.git", pushURL: "/tmp/remote.git", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ghPRRemoteConfigFromRemoteURLs(tt.fetchURL, tt.pushURL)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ghPRRemoteConfigFromRemoteURLs(%q, %q) error = nil, want error", tt.fetchURL, tt.pushURL)
				}
				return
			}
			if err != nil {
				t.Fatalf("ghPRRemoteConfigFromRemoteURLs(%q, %q) error = %v", tt.fetchURL, tt.pushURL, err)
			}
			if got.BaseRepoSelector != tt.wantBase {
				t.Fatalf("base repo = %q, want %q", got.BaseRepoSelector, tt.wantBase)
			}
			if got.HeadOwner != tt.wantHead {
				t.Fatalf("head owner = %q, want %q", got.HeadOwner, tt.wantHead)
			}
		})
	}
}

func remoteHasBranch(t *testing.T, remoteDir, branch string) bool {
	t.Helper()
	cmd := exec.Command("git", "--git-dir", remoteDir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	err := cmd.Run()
	if err == nil {
		return true
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false
	}
	t.Fatalf("show-ref(%s) error = %v", branch, err)
	return false
}

func remoteBranchCommit(t *testing.T, remoteDir, branch string) string {
	t.Helper()
	cmd := exec.Command("git", "--git-dir", remoteDir, "rev-parse", "refs/heads/"+branch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse(%s) error = %v\n%s", branch, err, string(output))
	}
	return strings.TrimSpace(string(output))
}

func TestNormalizeRemoteRefToBranchStripsConfiguredRemotePrefixes(t *testing.T) {
	requireGit(t)

	repoRoot := initRegisteredRepo(t, t.TempDir())
	runGit(t, repoRoot, "remote", "add", "team", "https://github.com/amux-test/team.git")

	if got := normalizeRemoteRefToBranch(repoRoot, "refs/remotes/team/main"); got != "main" {
		t.Fatalf("normalizeRemoteRefToBranch(refs/remotes/team/main) = %q, want %q", got, "main")
	}
	if got := normalizeRemoteRefToBranch(repoRoot, "team/main"); got != "main" {
		t.Fatalf("normalizeRemoteRefToBranch(team/main) = %q, want %q", got, "main")
	}
	if got := normalizeRemoteRefToBranch(repoRoot, "feature/api"); got != "feature/api" {
		t.Fatalf("normalizeRemoteRefToBranch(feature/api) = %q, want %q", got, "feature/api")
	}
}
