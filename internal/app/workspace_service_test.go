package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
)

// fakeProjectRegistry is a minimal ProjectRegistry stub that lets tests drive
// RemoveProject down its success/error paths without touching the filesystem.
type fakeProjectRegistry struct {
	removeErr     error
	removedPaths  []string
	removeCalls   int
	addErr        error
	projectsValue []string
	projectsErr   error
}

func (f *fakeProjectRegistry) Projects() ([]string, error) {
	return f.projectsValue, f.projectsErr
}

func (f *fakeProjectRegistry) AddProject(path string) error {
	return f.addErr
}

func (f *fakeProjectRegistry) RemoveProject(path string) error {
	f.removeCalls++
	f.removedPaths = append(f.removedPaths, path)
	return f.removeErr
}

// TestRemoveProjectNilProject covers the early-return guard: a nil project must
// surface messages.Error without ever touching the registry.
func TestRemoveProjectNilProject(t *testing.T) {
	reg := &fakeProjectRegistry{}
	svc := newWorkspaceService(reg, nil, nil, "")

	msg := svc.RemoveProject(nil)()

	errMsg, ok := msg.(messages.Error)
	if !ok {
		t.Fatalf("expected messages.Error, got %T", msg)
	}
	if errMsg.Err == nil || errMsg.Err.Error() != "missing project" {
		t.Fatalf("expected 'missing project' error, got %v", errMsg.Err)
	}
	if reg.removeCalls != 0 {
		t.Fatalf("registry.RemoveProject should not be called for nil project, got %d calls", reg.removeCalls)
	}
}

// TestRemoveProjectNilRegistry covers the registry-unavailable guard inside the
// returned command: a service whose registry is nil reports the failure rather
// than panicking.
func TestRemoveProjectNilRegistry(t *testing.T) {
	svc := newWorkspaceService(nil, nil, nil, "")
	project := data.NewProject("/tmp/repo")

	msg := svc.RemoveProject(project)()

	errMsg, ok := msg.(messages.Error)
	if !ok {
		t.Fatalf("expected messages.Error, got %T", msg)
	}
	if errMsg.Err == nil || errMsg.Err.Error() != "registry unavailable" {
		t.Fatalf("expected 'registry unavailable' error, got %v", errMsg.Err)
	}
}

// TestRemoveProjectNilService guards the s == nil branch: the returned command
// must still run without panicking and report the registry as unavailable.
func TestRemoveProjectNilService(t *testing.T) {
	var svc *workspaceService
	project := data.NewProject("/tmp/repo")

	msg := svc.RemoveProject(project)()

	errMsg, ok := msg.(messages.Error)
	if !ok {
		t.Fatalf("expected messages.Error, got %T", msg)
	}
	if errMsg.Err == nil || errMsg.Err.Error() != "registry unavailable" {
		t.Fatalf("expected 'registry unavailable' error, got %v", errMsg.Err)
	}
}

// TestRemoveProjectRegistryError covers the registry failure path: the error is
// propagated verbatim inside messages.Error with the workspace-service context.
func TestRemoveProjectRegistryError(t *testing.T) {
	wantErr := errors.New("registry write failed")
	reg := &fakeProjectRegistry{removeErr: wantErr}
	svc := newWorkspaceService(reg, nil, nil, "")
	project := data.NewProject("/tmp/repo")

	msg := svc.RemoveProject(project)()

	errMsg, ok := msg.(messages.Error)
	if !ok {
		t.Fatalf("expected messages.Error, got %T", msg)
	}
	if !errors.Is(errMsg.Err, wantErr) {
		t.Fatalf("expected wrapped registry error, got %v", errMsg.Err)
	}
	if reg.removeCalls != 1 {
		t.Fatalf("expected exactly one registry.RemoveProject call, got %d", reg.removeCalls)
	}
	if len(reg.removedPaths) != 1 || reg.removedPaths[0] != project.Path {
		t.Fatalf("expected RemoveProject called with %q, got %v", project.Path, reg.removedPaths)
	}
}

// TestRemoveProjectSuccess covers the happy path against the fake registry: the
// returned command emits ProjectRemoved carrying the project's path.
func TestRemoveProjectSuccess(t *testing.T) {
	reg := &fakeProjectRegistry{}
	svc := newWorkspaceService(reg, nil, nil, "")
	project := data.NewProject("/tmp/repo")

	msg := svc.RemoveProject(project)()

	removed, ok := msg.(messages.ProjectRemoved)
	if !ok {
		t.Fatalf("expected messages.ProjectRemoved, got %T", msg)
	}
	if removed.Path != project.Path {
		t.Fatalf("expected ProjectRemoved.Path = %q, got %q", project.Path, removed.Path)
	}
	if reg.removeCalls != 1 {
		t.Fatalf("expected exactly one registry.RemoveProject call, got %d", reg.removeCalls)
	}
	if len(reg.removedPaths) != 1 || reg.removedPaths[0] != project.Path {
		t.Fatalf("expected RemoveProject called with %q, got %v", project.Path, reg.removedPaths)
	}
}

func TestRemoveProjectStopsProjectScriptsAndReleasesPorts(t *testing.T) {
	repo := t.TempDir()
	workspaceRoot := t.TempDir()
	ws := data.NewWorkspace("feature", "feature", "main", repo, workspaceRoot)
	ws.ScriptMode = "nonconcurrent"
	ws.Scripts.Run = "sleep 30"

	runner := process.NewScriptRunner(6300, 10)
	t.Cleanup(func() { _ = runner.Stop(ws) })
	if _, err := runner.RunScript(ws, process.ScriptRun); err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if !runner.IsRunning(ws) {
		t.Fatal("expected project script to be running before removal")
	}
	if _, allocated := runner.PortAllocated(ws); !allocated {
		t.Fatal("expected project port to be allocated before removal")
	}

	reg := &fakeProjectRegistry{}
	svc := newWorkspaceService(reg, nil, runner, "")
	project := data.NewProject(repo)
	project.Workspaces = []data.Workspace{*ws}
	msg := svc.RemoveProject(project)()
	if _, ok := msg.(messages.ProjectRemoved); !ok {
		t.Fatalf("expected ProjectRemoved, got %T", msg)
	}
	if runner.IsRunning(ws) {
		t.Fatal("project script remained running after removal")
	}
	if _, allocated := runner.PortAllocated(ws); allocated {
		t.Fatal("project port remained allocated after removal")
	}
}

// TestRemoveProjectSuccessWithRealRegistry exercises the success path end-to-end
// against a real on-disk registry: a registered project is unregistered and the
// projects.json no longer lists it.
func TestRemoveProjectSuccessWithRealRegistry(t *testing.T) {
	dir := t.TempDir()
	registry := data.NewRegistry(filepath.Join(dir, "projects.json"))
	if err := registry.AddProject("/path/to/keep"); err != nil {
		t.Fatalf("AddProject(keep): %v", err)
	}
	if err := registry.AddProject("/path/to/drop"); err != nil {
		t.Fatalf("AddProject(drop): %v", err)
	}

	svc := newWorkspaceService(registry, nil, nil, "")
	project := data.NewProject("/path/to/drop")

	msg := svc.RemoveProject(project)()

	removed, ok := msg.(messages.ProjectRemoved)
	if !ok {
		t.Fatalf("expected messages.ProjectRemoved, got %T", msg)
	}
	if removed.Path != project.Path {
		t.Fatalf("expected ProjectRemoved.Path = %q, got %q", project.Path, removed.Path)
	}

	paths, err := registry.Load()
	if err != nil {
		t.Fatalf("registry.Load: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected exactly one project remaining, got %d (%v)", len(paths), paths)
	}
	if data.NormalizePath(paths[0]) != data.NormalizePath("/path/to/keep") {
		t.Fatalf("expected remaining project /path/to/keep, got %q", paths[0])
	}
}

func TestRemoveProjectCleansMetadataAndSessionsButLeavesFiles(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	workspaceRoot := filepath.Join(dir, "workspaces", "repo", "feature")
	if err := os.MkdirAll(workspaceRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	registry := data.NewRegistry(filepath.Join(dir, "projects.json"))
	if err := registry.AddProject(repo); err != nil {
		t.Fatal(err)
	}
	store := data.NewWorkspaceStore(filepath.Join(dir, "metadata"))
	ws := data.NewWorkspace("feature", "feature", "main", repo, workspaceRoot)
	if err := store.Save(ws); err != nil {
		t.Fatal(err)
	}

	svc := newWorkspaceService(registry, store, nil, filepath.Join(dir, "workspaces"))
	var killed []string
	svc.killWorkspaceSessions = func(id string) { killed = append(killed, id) }
	msg := svc.RemoveProject(data.NewProject(repo))()
	if _, ok := msg.(messages.ProjectRemoved); !ok {
		t.Fatalf("expected ProjectRemoved, got %T", msg)
	}
	if _, err := store.Load(ws.ID()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workspace metadata still exists, err = %v", err)
	}
	if len(killed) != 1 || killed[0] != string(ws.ID()) {
		t.Fatalf("killed sessions = %v, want [%s]", killed, ws.ID())
	}
	if _, err := os.Stat(workspaceRoot); err != nil {
		t.Fatalf("RemoveProject deleted workspace files: %v", err)
	}
}

func TestRemoveProjectCleansKnownSessionsWithoutMetadataStore(t *testing.T) {
	repo := t.TempDir()
	ws := data.NewWorkspace("feature", "feature", "main", repo, filepath.Join(repo, "feature"))
	project := data.NewProject(repo)
	project.Workspaces = []data.Workspace{*ws}

	svc := newWorkspaceService(&fakeProjectRegistry{}, nil, nil, "")
	var killed []string
	svc.killWorkspaceSessions = func(id string) { killed = append(killed, id) }
	msg := svc.RemoveProject(project)()
	if _, ok := msg.(messages.ProjectRemoved); !ok {
		t.Fatalf("expected ProjectRemoved, got %T", msg)
	}
	if len(killed) != 1 || killed[0] != string(ws.ID()) {
		t.Fatalf("killed sessions = %v, want [%s]", killed, ws.ID())
	}
}

func TestPruneMissingTemporaryProjectsKeepsNonTemporaryMissingPaths(t *testing.T) {
	missingTemp := filepath.Join(t.TempDir(), "gone", "repo")
	missingNonTemp := filepath.Join(string(filepath.Separator), "amux-missing-volume", "repo")
	registry := &fakeProjectRegistry{}
	svc := newWorkspaceService(registry, nil, nil, "")

	kept := svc.pruneMissingTemporaryProjects([]string{missingTemp, missingNonTemp})
	if len(registry.removedPaths) != 1 || registry.removedPaths[0] != missingTemp {
		t.Fatalf("removed paths = %v, want only %s", registry.removedPaths, missingTemp)
	}
	if len(kept) != 1 || kept[0] != missingNonTemp {
		t.Fatalf("kept paths = %v, want only %s", kept, missingNonTemp)
	}
}

func TestLoadProjectsReconcilesMetadataOnlyOnInitialLoad(t *testing.T) {
	root := t.TempDir()
	metadataRoot := filepath.Join(root, "metadata")
	store := data.NewWorkspaceStore(metadataRoot)
	ws := data.NewWorkspace(
		"stale",
		"stale",
		"main",
		filepath.Join(root, "unregistered-repo"),
		filepath.Join(root, "workspaces", "stale"),
	)
	if err := store.Save(ws); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(metadataRoot, string(ws.ID()), "workspace.json")
	old := time.Now().Add(-metadataOrphanGracePeriod - time.Hour)
	if err := os.Chtimes(metadataPath, old, old); err != nil {
		t.Fatal(err)
	}

	svc := newWorkspaceService(&fakeProjectRegistry{}, store, nil, filepath.Join(root, "workspaces"))
	_ = svc.LoadProjects(2)()
	if _, err := store.Load(ws.ID()); err != nil {
		t.Fatalf("non-initial refresh pruned metadata: %v", err)
	}

	_ = svc.LoadProjects(1)()
	if _, err := store.Load(ws.ID()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("initial load did not reconcile stale metadata: %v", err)
	}
}

func TestPruneMissingTemporaryProjectCleansOwnedStateButLeavesWorkspaceFiles(t *testing.T) {
	root := t.TempDir()
	missingRepo := filepath.Join(root, "gone", "repo")
	workspaceRoot := filepath.Join(root, "workspaces", "repo", "feature")
	if err := os.MkdirAll(workspaceRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	registry := data.NewRegistry(filepath.Join(root, "projects.json"))
	if err := registry.AddProject(missingRepo); err != nil {
		t.Fatal(err)
	}
	store := data.NewWorkspaceStore(filepath.Join(root, "metadata"))
	ws := data.NewWorkspace("feature", "feature", "main", missingRepo, workspaceRoot)
	if err := store.Save(ws); err != nil {
		t.Fatal(err)
	}

	svc := newWorkspaceService(registry, store, nil, filepath.Join(root, "workspaces"))
	var killed []string
	svc.killWorkspaceSessions = func(id string) { killed = append(killed, id) }
	if kept := svc.pruneMissingTemporaryProjects([]string{missingRepo}); len(kept) != 0 {
		t.Fatalf("kept vanished temp project: %v", kept)
	}
	if _, err := store.Load(ws.ID()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workspace metadata still exists, err = %v", err)
	}
	if len(killed) != 1 || killed[0] != string(ws.ID()) {
		t.Fatalf("killed sessions = %v, want [%s]", killed, ws.ID())
	}
	if _, err := os.Stat(workspaceRoot); err != nil {
		t.Fatalf("automatic registry cleanup deleted workspace files: %v", err)
	}
}

// TestRemoveProjectRealRegistryUnknownPathIsNoop documents that removing a path
// the registry never tracked is a benign no-op that still emits ProjectRemoved.
func TestRemoveProjectRealRegistryUnknownPathIsNoop(t *testing.T) {
	dir := t.TempDir()
	registry := data.NewRegistry(filepath.Join(dir, "projects.json"))
	if err := registry.AddProject("/path/to/keep"); err != nil {
		t.Fatalf("AddProject(keep): %v", err)
	}

	svc := newWorkspaceService(registry, nil, nil, "")
	project := data.NewProject("/path/never/registered")

	msg := svc.RemoveProject(project)()

	if _, ok := msg.(messages.ProjectRemoved); !ok {
		t.Fatalf("expected messages.ProjectRemoved, got %T", msg)
	}

	paths, err := registry.Load()
	if err != nil {
		t.Fatalf("registry.Load: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected the registered project to be untouched, got %v", paths)
	}
}

// TestStopAllNilService guards the s == nil branch: calling StopAll on a nil
// receiver must not panic.
func TestStopAllNilService(t *testing.T) {
	var svc *workspaceService
	svc.StopAll() // must not panic
}

// TestStopAllNilScripts guards the s.scripts == nil branch: a service without a
// ScriptRunner is a safe no-op.
func TestStopAllNilScripts(t *testing.T) {
	svc := newWorkspaceService(nil, nil, nil, "")
	if svc.scripts != nil {
		t.Fatalf("expected nil scripts for service built without a ScriptRunner")
	}
	svc.StopAll() // must not panic
}

// TestStopAllWithIdleScriptRunner exercises the delegation branch with a real
// ScriptRunner that has no running scripts: StopAll forwards to the runner and
// completes without spawning or killing any external process, and remains
// idempotent across repeated calls.
func TestStopAllWithIdleScriptRunner(t *testing.T) {
	runner := process.NewScriptRunner(6300, 10)
	svc := newWorkspaceService(nil, nil, runner, "")

	// No scripts are running, so this is a pure map-clear with no exec.
	svc.StopAll()
	svc.StopAll() // idempotent: a second call is still a no-op.
}
