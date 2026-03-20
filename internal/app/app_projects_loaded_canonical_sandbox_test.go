package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/sandbox"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

func TestHandleProjectsLoadedCanonicalRebindRetargetsSandboxMetadataWhileStayingInSandbox(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	base := t.TempDir()
	absRepo := filepath.Join(base, "repo")
	absRoot := filepath.Join(base, "workspaces", "repo", "feature")
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", absRoot, err)
	}

	relRepo, err := filepath.Rel(wd, absRepo)
	if err != nil {
		t.Fatalf("Rel(repo): %v", err)
	}
	relRoot, err := filepath.Rel(wd, absRoot)
	if err != nil {
		t.Fatalf("Rel(root): %v", err)
	}

	oldWS := data.NewWorkspace("feature", "feat-branch", "main", relRepo, relRoot)
	oldWS.Runtime = data.RuntimeCloudSandbox
	oldProject := data.NewProject(relRepo)
	oldProject.AddWorkspace(*oldWS)

	newWS := data.NewWorkspace("feature", "feat-branch", "main", absRepo, absRoot)
	newWS.Runtime = data.RuntimeCloudSandbox
	newProject := data.NewProject(absRepo)
	newProject.AddWorkspace(*newWS)

	needsSync := true
	if err := sandbox.SaveSandboxMeta(oldWS.Root, "fake", sandbox.SandboxMeta{
		SandboxID:     "sb-canonical-cloud",
		Agent:         sandbox.AgentShell,
		Provider:      "fake",
		WorktreeID:    sandbox.ComputeWorktreeID(oldWS.Root),
		NeedsSyncDown: &needsSync,
		WorkspaceIDs:  []string{string(oldWS.ID())},
	}); err != nil {
		t.Fatalf("SaveSandboxMeta(): %v", err)
	}

	manager := NewSandboxManager(nil)
	session := &sandboxSession{
		sandbox:            sandbox.NewMockRemoteSandbox("sb-canonical-cloud"),
		providerName:       "fake",
		worktreeID:         sandbox.ComputeWorktreeID(oldWS.Root),
		workspaceID:        oldWS.ID(),
		workspaceIDAliases: map[string]struct{}{string(oldWS.ID()): {}},
		workspaceRoot:      oldWS.Root,
		workspaceRepo:      oldWS.Repo,
		workspacePath:      "/home/daytona/.amux/workspaces/canonical/repo",
	}
	manager.storeSession(session)

	app := &App{
		dashboard:       dashboard.New(),
		projects:        []data.Project{*oldProject},
		activeWorkspace: &oldProject.Workspaces[0],
		activeProject:   oldProject,
		showWelcome:     false,
		sandboxManager:  manager,
	}

	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*newProject}})

	if session.workspaceRoot != absRoot {
		t.Fatalf("session.workspaceRoot = %q, want %q", session.workspaceRoot, absRoot)
	}
	metaNew, err := sandbox.LoadSandboxMeta(absRoot, "fake")
	if err != nil {
		t.Fatalf("LoadSandboxMeta(new): %v", err)
	}
	if metaNew == nil || metaNew.SandboxID != "sb-canonical-cloud" {
		t.Fatalf("new metadata = %#v, want sandbox metadata moved to canonical root", metaNew)
	}
	metaOld, err := sandbox.LoadSandboxMeta(oldWS.Root, "fake")
	if err != nil {
		t.Fatalf("LoadSandboxMeta(old): %v", err)
	}
	if sandbox.ComputeWorktreeID(oldWS.Root) != sandbox.ComputeWorktreeID(absRoot) && metaOld != nil {
		t.Fatalf("expected old-root metadata to be moved, got %#v", metaOld)
	}
}
