package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestCmdWorkspacePRAllowsDirtyWorktree(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot, _ := initRegisteredRepoWithOrigin(t, home)

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
	if err := os.WriteFile(filepath.Join(workspace.Root, "feature.txt"), []byte("feature-1\ndirty\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(dirty feature.txt) error = %v", err)
	}

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
	if code != ExitOK {
		t.Fatalf("cmdWorkspacePR(dirty) code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	prStore := loadFakePRStore(t, storePath)
	if _, ok := prStore.PRs[workspace.Branch]; !ok {
		t.Fatalf("expected PR to be created for dirty workspace branch %q", workspace.Branch)
	}
}

func TestCmdWorkspacePRRejectsNonFastForwardPush(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot, remoteDir := initRegisteredRepoWithOrigin(t, home)

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

	_, restore := stubGHCLIForTest(t)
	defer restore()

	var out, errOut bytes.Buffer
	code := cmdWorkspacePR(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{workspace.ID},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("initial cmdWorkspacePR() code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	otherClone := filepath.Join(t.TempDir(), "other")
	runGit(t, t.TempDir(), "clone", remoteDir, otherClone)
	runGit(t, otherClone, "config", "user.email", "test@example.com")
	runGit(t, otherClone, "config", "user.name", "amux-test")
	runGit(t, otherClone, "checkout", workspace.Branch)
	if err := os.WriteFile(filepath.Join(otherClone, "remote.txt"), []byte("remote-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(remote-1) error = %v", err)
	}
	runGit(t, otherClone, "add", "remote.txt")
	runGit(t, otherClone, "commit", "-m", "remote 1")
	runGit(t, otherClone, "push", "origin", workspace.Branch)
	remoteHead := remoteBranchCommit(t, remoteDir, workspace.Branch)

	runGit(t, workspace.Root, "fetch", "origin", workspace.Branch)

	out.Reset()
	errOut.Reset()
	code = cmdWorkspacePR(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{workspace.ID},
		"test-v1",
	)
	if code != ExitInternalError {
		t.Fatalf("cmdWorkspacePR(non-fast-forward) code = %d, want %d; stderr: %s; stdout: %s", code, ExitInternalError, errOut.String(), out.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal(error) error = %v\nraw: %s", err, out.String())
	}
	if env.OK || env.Error == nil {
		t.Fatalf("expected error envelope, got %#v", env)
	}
	if env.Error.Code != "push_failed" {
		t.Fatalf("error code = %q, want %q", env.Error.Code, "push_failed")
	}
	if got := remoteBranchCommit(t, remoteDir, workspace.Branch); got != remoteHead {
		t.Fatalf("remote branch head = %q, want unchanged %q", got, remoteHead)
	}
}

func TestCmdWorkspacePRForcePushesRebasedParentBranchForDescendant(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot, remoteDir := initRegisteredRepoWithOrigin(t, home)

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

	grandchild := createWorkspaceForTest(
		t,
		"test-v1",
		"api", "--from-workspace", child.ID, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(grandchild.Root, "grandchild.txt"), []byte("grandchild-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(grandchild-1) error = %v", err)
	}
	runGit(t, grandchild.Root, "add", "grandchild.txt")
	runGit(t, grandchild.Root, "commit", "-m", "grandchild 1")

	_, restore := stubGHCLIForTest(t)
	defer restore()

	var out, errOut bytes.Buffer
	code := cmdWorkspacePR(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{root.ID, "--recursive"},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("initial cmdWorkspacePR(recursive) code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	childRemoteHead := remoteBranchCommit(t, remoteDir, child.Branch)

	alt := createWorkspaceForTest(
		t,
		"test-v1",
		"alt", "--project", repoRoot, "--assistant", "claude",
	)
	if err := os.WriteFile(filepath.Join(alt.Root, "alt.txt"), []byte("alt-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(alt-1) error = %v", err)
	}
	runGit(t, alt.Root, "add", "alt.txt")
	runGit(t, alt.Root, "commit", "-m", "alt 1")

	out.Reset()
	errOut.Reset()
	code = cmdWorkspaceReparent(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{child.ID, "--parent", alt.ID},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdWorkspaceReparent(child -> alt) code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	rebasedChildHead := runGitOutput(t, repoRoot, "rev-parse", child.Branch)
	if rebasedChildHead == childRemoteHead {
		t.Fatalf("expected child branch head to change after reparent")
	}

	out.Reset()
	errOut.Reset()
	code = cmdWorkspacePR(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{grandchild.ID},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdWorkspacePR(grandchild after parent reparent) code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	if got := remoteBranchCommit(t, remoteDir, child.Branch); got != rebasedChildHead {
		t.Fatalf("remote child branch head = %q, want rebased head %q", got, rebasedChildHead)
	}
}

func TestCmdWorkspacePRPrimaryParentUsesBaseRepo(t *testing.T) {
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

	store := data.NewWorkspaceStore(filepath.Join(home, ".amux", "workspaces-metadata"))
	primary := data.NewWorkspace("main", "main", "main", repoRoot, repoRoot)
	primary.BaseCommit = runGitOutput(t, repoRoot, "rev-parse", "main")
	if err := store.Save(primary); err != nil {
		t.Fatalf("store.Save(primary) error = %v", err)
	}

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

	var out, errOut bytes.Buffer
	code := cmdWorkspaceReparent(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{workspace.ID, "--parent", string(primary.ID())},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdWorkspaceReparent(primary parent) code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	storePath, restore := stubGHCLIForTest(t)
	defer restore()

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
		t.Fatalf("cmdWorkspacePR(primary parent, fork remote) code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	prStore := loadFakePRStore(t, storePath)
	sawUpstreamCreate := false
	for _, call := range prStore.Calls {
		if call.Subcommand != "create" {
			continue
		}
		if call.Repo == "fork-user/upstream" {
			t.Fatalf("unexpected stacked PR create against fork repo for primary-parent workspace: %#v", prStore.Calls)
		}
		if call.Repo == "amux-test/upstream" && call.Head == "fork-user:feature" {
			sawUpstreamCreate = true
		}
	}
	if !sawUpstreamCreate {
		t.Fatalf("expected PR create against upstream repo, calls = %#v", prStore.Calls)
	}
	if remoteHasBranch(t, forkDir, "main") {
		t.Fatalf("unexpected push of primary checkout branch to fork remote")
	}
	if !remoteHasBranch(t, forkDir, workspace.Branch) {
		t.Fatalf("expected feature branch %q to be pushed to fork remote", workspace.Branch)
	}
}

func TestCmdWorkspacePRStackedChildRejectsNonFastForwardWithoutPendingForcePush(t *testing.T) {
	requireGit(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot, remoteDir := initRegisteredRepoWithOrigin(t, home)

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

	_, restore := stubGHCLIForTest(t)
	defer restore()

	var out, errOut bytes.Buffer
	code := cmdWorkspacePR(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{root.ID, "--recursive"},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("initial cmdWorkspacePR(recursive) code = %d; stderr: %s; stdout: %s", code, errOut.String(), out.String())
	}

	reloadedChild := loadWorkspaceFromHome(t, home, child.ID)
	if reloadedChild.PendingForcePush {
		t.Fatalf("child pending_force_push unexpectedly true after initial publish")
	}

	otherClone := filepath.Join(t.TempDir(), "other")
	runGit(t, t.TempDir(), "clone", remoteDir, otherClone)
	runGit(t, otherClone, "config", "user.email", "test@example.com")
	runGit(t, otherClone, "config", "user.name", "amux-test")
	runGit(t, otherClone, "checkout", child.Branch)
	if err := os.WriteFile(filepath.Join(otherClone, "remote.txt"), []byte("remote-1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(remote-1) error = %v", err)
	}
	runGit(t, otherClone, "add", "remote.txt")
	runGit(t, otherClone, "commit", "-m", "remote 1")
	runGit(t, otherClone, "push", "origin", child.Branch)
	remoteHead := remoteBranchCommit(t, remoteDir, child.Branch)

	runGit(t, child.Root, "fetch", "origin", child.Branch)

	out.Reset()
	errOut.Reset()
	code = cmdWorkspacePR(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{child.ID},
		"test-v1",
	)
	if code != ExitInternalError {
		t.Fatalf("cmdWorkspacePR(stacked child non-fast-forward) code = %d, want %d; stderr: %s; stdout: %s", code, ExitInternalError, errOut.String(), out.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal(error) error = %v\nraw: %s", err, out.String())
	}
	if env.OK || env.Error == nil {
		t.Fatalf("expected error envelope, got %#v", env)
	}
	if env.Error.Code != "push_failed" {
		t.Fatalf("error code = %q, want %q", env.Error.Code, "push_failed")
	}
	if got := remoteBranchCommit(t, remoteDir, child.Branch); got != remoteHead {
		t.Fatalf("remote child branch head = %q, want unchanged %q", got, remoteHead)
	}
}
