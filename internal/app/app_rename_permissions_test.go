package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/medusa/internal/config"
	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/messages"
	"github.com/andyrewlee/medusa/internal/permissions"
	"github.com/andyrewlee/medusa/internal/ui/center"
	"github.com/andyrewlee/medusa/internal/ui/common"
)

// newTestApp builds a minimal App suitable for rename tests. It wires up only
// the fields that the rename handlers touch (config, workspaces store, center,
// toast, permissionWatcher, dirtyWorkspaces). Everything else is left nil/zero,
// which is safe because the rename handlers nil-check optional components.
func newTestApp(t *testing.T) (*App, *config.Config) {
	t.Helper()
	// Resolve symlinks so that paths are stable even after directories
	// are moved/deleted (macOS /var -> /private/var).
	tmp := normalizePath(t.TempDir())
	cfg := &config.Config{
		Paths: &config.Paths{
			Home:                  tmp,
			WorkspacesRoot:        filepath.Join(tmp, "workspaces"),
			GroupsWorkspacesRoot:  filepath.Join(tmp, "workspaces", "groups"),
			MetadataRoot:          filepath.Join(tmp, "workspaces-metadata"),
			RegistryPath:          filepath.Join(tmp, "projects.json"),
			ProfilesRoot:          filepath.Join(tmp, "profiles"),
			GlobalPermissionsPath: filepath.Join(tmp, "global_permissions.json"),
		},
	}
	store := data.NewWorkspaceStore(cfg.Paths.MetadataRoot)
	registry := data.NewRegistry(cfg.Paths.RegistryPath)
	pw, err := permissions.NewPermissionWatcher(func(string, []string) {})
	if err != nil {
		t.Fatalf("NewPermissionWatcher: %v", err)
	}
	t.Cleanup(func() { pw.Close() })

	app := &App{
		config:            cfg,
		registry:          registry,
		workspaces:        store,
		center:            center.New(cfg),
		toast:             common.NewToastModel(),
		permissionWatcher: pw,
		dirtyWorkspaces:   make(map[string]bool),
	}
	return app, cfg
}

func TestRenameWorkspace_UpdatesPermissionWatcher(t *testing.T) {
	skipIfNoGit(t)

	app, _ := newTestApp(t)
	pw := app.permissionWatcher

	// Set up a git repo with a worktree.
	repo := normalizePath(t.TempDir())
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	worktreeDir := normalizePath(t.TempDir())
	oldName := "old-feature"
	oldRoot := filepath.Join(worktreeDir, oldName)
	runGit(t, repo, "worktree", "add", "--no-track", "-b", oldName, oldRoot, "main")

	// Persist workspace into the store so handleRenameWorkspace can Load it.
	ws := &data.Workspace{
		Name:    oldName,
		Branch:  oldName,
		Repo:    repo,
		Root:    oldRoot,
		Created: time.Now(),
		Runtime: data.RuntimeLocalWorktree,
	}
	if err := app.workspaces.Save(ws); err != nil {
		t.Fatalf("Save workspace: %v", err)
	}

	// Register the old root with the permission watcher.
	if err := pw.Watch(oldRoot); err != nil {
		t.Fatalf("Watch old root: %v", err)
	}
	if !pw.IsWatching(oldRoot) {
		t.Fatal("expected permission watcher to be watching old root before rename")
	}

	// Rename the workspace.
	project := &data.Project{Path: repo, Name: filepath.Base(repo)}
	newName := "new-feature"
	cmds := app.handleRenameWorkspace(messages.RenameWorkspace{
		Project:   project,
		Workspace: ws,
		NewName:   newName,
	})
	if len(cmds) == 0 {
		t.Fatal("handleRenameWorkspace returned no commands (rename likely failed)")
	}

	newRoot := filepath.Join(worktreeDir, newName)
	if pw.IsWatching(oldRoot) {
		t.Errorf("permission watcher still watching old root %s after rename", oldRoot)
	}
	if !pw.IsWatching(newRoot) {
		t.Errorf("permission watcher not watching new root %s after rename", newRoot)
	}
}

func TestRenameGroupWorkspace_UpdatesPermissionWatcher(t *testing.T) {
	skipIfNoGit(t)

	app, cfg := newTestApp(t)
	pw := app.permissionWatcher

	// Create two repos to act as group repos.
	repo1 := normalizePath(t.TempDir())
	runGit(t, repo1, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo1, "README.md"), []byte("r1\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo1, "add", "README.md")
	runGit(t, repo1, "commit", "-m", "init")

	repo2 := normalizePath(t.TempDir())
	runGit(t, repo2, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo2, "README.md"), []byte("r2\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo2, "add", "README.md")
	runGit(t, repo2, "commit", "-m", "init")

	// Build worktree structure under groups root:
	//   groups/<groupName>/<wsName>/<repoBase>/
	groupName := "mygroup"
	oldWsName := "old-gws"
	groupDir := filepath.Join(cfg.Paths.GroupsWorkspacesRoot, groupName)
	oldGroupRoot := filepath.Join(groupDir, oldWsName)
	repo1Base := filepath.Base(repo1)
	repo2Base := filepath.Base(repo2)
	oldSecRoot1 := filepath.Join(oldGroupRoot, repo1Base)
	oldSecRoot2 := filepath.Join(oldGroupRoot, repo2Base)

	if err := os.MkdirAll(oldGroupRoot, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runGit(t, repo1, "worktree", "add", "--no-track", "-b", oldWsName, oldSecRoot1, "main")
	runGit(t, repo2, "worktree", "add", "--no-track", "-b", oldWsName, oldSecRoot2, "main")

	primary := data.Workspace{
		Name:   oldWsName,
		Branch: oldWsName,
		Repo:   repo1,
		Root:   oldGroupRoot,
	}
	secondary := []data.Workspace{
		{Name: oldWsName, Branch: oldWsName, Repo: repo1, Root: oldSecRoot1},
		{Name: oldWsName, Branch: oldWsName, Repo: repo2, Root: oldSecRoot2},
	}
	gw := &data.GroupWorkspace{
		GroupName: groupName,
		Name:      oldWsName,
		Primary:   primary,
		Secondary: secondary,
	}

	// Save group workspace so the rename handler can load it.
	if err := app.workspaces.SaveGroupWorkspace(gw); err != nil {
		t.Fatalf("SaveGroupWorkspace: %v", err)
	}

	// Register secondary roots with permission watcher.
	for _, root := range []string{oldSecRoot1, oldSecRoot2} {
		if err := pw.Watch(root); err != nil {
			t.Fatalf("Watch %s: %v", root, err)
		}
	}

	// Sanity check.
	if !pw.IsWatching(oldSecRoot1) || !pw.IsWatching(oldSecRoot2) {
		t.Fatal("expected permission watcher to be watching old secondary roots before rename")
	}

	// Rename the group workspace.
	newWsName := "new-gws"
	group := &data.ProjectGroup{Name: groupName}
	cmds := app.handleRenameGroupWorkspace(messages.RenameGroupWorkspace{
		Group:     group,
		Workspace: gw,
		NewName:   newWsName,
	})
	if len(cmds) == 0 {
		t.Fatal("handleRenameGroupWorkspace returned no commands (rename likely failed)")
	}

	newGroupRoot := filepath.Join(groupDir, newWsName)
	newSecRoot1 := filepath.Join(newGroupRoot, repo1Base)
	newSecRoot2 := filepath.Join(newGroupRoot, repo2Base)

	// Old roots must no longer be watched.
	if pw.IsWatching(oldSecRoot1) {
		t.Errorf("permission watcher still watching old secondary root %s", oldSecRoot1)
	}
	if pw.IsWatching(oldSecRoot2) {
		t.Errorf("permission watcher still watching old secondary root %s", oldSecRoot2)
	}
	// New roots must be watched.
	if !pw.IsWatching(newSecRoot1) {
		t.Errorf("permission watcher not watching new secondary root %s", newSecRoot1)
	}
	if !pw.IsWatching(newSecRoot2) {
		t.Errorf("permission watcher not watching new secondary root %s", newSecRoot2)
	}
}
