package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// newProjectEnvHarness returns a harness App with a real temp-dir-backed
// ProjectEnvStore attached (newAppShell leaves it nil — app_init wires it).
func newProjectEnvHarness(t *testing.T) (*Harness, *data.ProjectEnvStore) {
	t.Helper()
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	store := data.NewProjectEnvStore(t.TempDir())
	h.app.projectEnvStore = store
	return h, store
}

func TestHandleShowProjectEnvDialog_SeedsFromStore(t *testing.T) {
	h, store := newProjectEnvHarness(t)
	repo := t.TempDir()
	if err := store.Set(repo, map[string]string{"API_KEY": "secret", "AMUX_PORT": "poison"}); err != nil {
		t.Fatalf("seed Set() error = %v", err)
	}
	ws := &data.Workspace{Name: "ws", Repo: repo, Root: repo + "/ws"}

	h.app.handleShowProjectEnvDialog(messages.ShowProjectEnvDialog{Workspace: ws})

	if h.app.overlays.projectEnv == nil || !h.app.overlays.projectEnv.Visible() {
		t.Fatal("expected projectEnvDialog shown")
	}
	if h.app.overlays.projectEnvRepo != repo {
		t.Fatalf("projectEnvRepo = %q, want %q", h.app.overlays.projectEnvRepo, repo)
	}
	got := h.app.overlays.projectEnv.Env()
	if got["API_KEY"] != "secret" {
		t.Fatalf("expected API_KEY row, got %#v", got)
	}
	if _, ok := got["AMUX_PORT"]; ok {
		t.Fatalf("reserved key must never render as an editable row, got %#v", got)
	}
	if !strings.Contains(h.app.overlays.projectEnv.View(), "Project Environment") {
		t.Fatalf("dialog must identify itself as project-scoped, got %q", h.app.overlays.projectEnv.View())
	}
}

func TestHandleShowProjectEnvDialog_NilInputsAreNoop(t *testing.T) {
	h, _ := newProjectEnvHarness(t)
	h.app.handleShowProjectEnvDialog(messages.ShowProjectEnvDialog{Workspace: nil})
	if h.app.overlays.projectEnv != nil {
		t.Fatal("dialog shown for nil workspace")
	}
	h.app.projectEnvStore = nil
	h.app.handleShowProjectEnvDialog(messages.ShowProjectEnvDialog{Workspace: &data.Workspace{Repo: "/r"}})
	if h.app.overlays.projectEnv != nil {
		t.Fatal("dialog shown with no store")
	}
}

func TestHandleProjectEnvDialogResult_PersistsToProjectStore(t *testing.T) {
	h, store := newProjectEnvHarness(t)
	repo := t.TempDir()
	ws := &data.Workspace{Name: "ws", Repo: repo, Root: repo + "/ws"}
	if err := store.Set(repo, map[string]string{"NODE_ENV": "dev"}); err != nil {
		t.Fatalf("seed Set() error = %v", err)
	}

	h.app.handleShowProjectEnvDialog(messages.ShowProjectEnvDialog{Workspace: ws})
	h.app.overlays.projectEnv.Update(tea.KeyPressMsg{Code: 'X', Text: "X"})

	cmd := h.app.handleProjectEnvDialogResult(common.EnvDialogResult{})
	if cmd == nil {
		t.Fatal("expected a success-toast cmd")
	}
	got := store.ForRepo(repo)
	if got["NODE_ENV"] != "devX" {
		t.Fatalf("persisted NODE_ENV = %q, want %q", got["NODE_ENV"], "devX")
	}
	if h.app.overlays.projectEnv != nil || h.app.overlays.projectEnvRepo != "" {
		t.Fatal("dialog state not cleared after confirm")
	}
}

func TestHandleShowProjectEnvDialog_SharedAcrossWorkspaces(t *testing.T) {
	// Two workspaces of the same repo read the one project map — the point
	// of the layer.
	h, store := newProjectEnvHarness(t)
	repo := t.TempDir()
	if err := store.Set(repo, map[string]string{"SHARED_KEY": "shared"}); err != nil {
		t.Fatalf("seed Set() error = %v", err)
	}
	wsA := &data.Workspace{Name: "a", Repo: repo, Root: repo + "/a"}
	wsB := &data.Workspace{Name: "b", Repo: repo, Root: repo + "/b"}

	h.app.handleShowProjectEnvDialog(messages.ShowProjectEnvDialog{Workspace: wsA})
	h.app.handleProjectEnvDialogResult(common.EnvDialogResult{Canceled: true})

	h.app.handleShowProjectEnvDialog(messages.ShowProjectEnvDialog{Workspace: wsB})
	if got := h.app.overlays.projectEnv.Env(); got["SHARED_KEY"] != "shared" {
		t.Fatalf("wsB's dialog seeded %#v, want the repo-level map wsA shares", got)
	}
}

func TestHandleProjectEnvDialogResult_CanceledDiscardsEdits(t *testing.T) {
	h, store := newProjectEnvHarness(t)
	repo := t.TempDir()
	ws := &data.Workspace{Name: "ws", Repo: repo, Root: repo + "/ws"}
	if err := store.Set(repo, map[string]string{"NODE_ENV": "dev"}); err != nil {
		t.Fatalf("seed Set() error = %v", err)
	}

	h.app.handleShowProjectEnvDialog(messages.ShowProjectEnvDialog{Workspace: ws})
	h.app.overlays.projectEnv.Update(tea.KeyPressMsg{Code: 'X', Text: "X"})
	if cmd := h.app.handleProjectEnvDialogResult(common.EnvDialogResult{Canceled: true}); cmd != nil {
		t.Fatalf("cancel should emit no cmd, got one that emits %T", cmd())
	}
	if got := store.ForRepo(repo); got["NODE_ENV"] != "dev" {
		t.Fatalf("cancel must not persist: %#v", got)
	}
}

func TestHandleProjectEnvDialogResult_NoDialogIsNoop(t *testing.T) {
	h, _ := newProjectEnvHarness(t)
	if cmd := h.app.handleProjectEnvDialogResult(common.EnvDialogResult{}); cmd != nil {
		t.Fatalf("expected nil cmd with no dialog open, got one that emits %T", cmd())
	}
}

// TestSessionEnvCarriesProjectStoreValues composes the same seam New wires
// (ScriptRunner + SetProjectEnvResolver(app.projectEnvStore.ForRepo)) and
// asserts a value persisted through the app's dialog path reaches the
// interactive-session env BuildSessionEnv produces — agent terminals and
// sidebar terminals consume it via SetSessionEnvProvider — while the repo
// config's env layer never does (script trust does not widen to sessions).
func TestSessionEnvCarriesProjectStoreValues(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".amux"), 0o755); err != nil {
		t.Fatalf("mkdir .amux: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".amux", "workspaces.json"),
		[]byte(`{"env": {"REPO_ONLY": "r", "SHARED": "repo"}}`), 0o644); err != nil {
		t.Fatalf("write workspaces.json: %v", err)
	}

	h, store := newProjectEnvHarness(t)
	if err := store.Set(repo, map[string]string{"PROJ_VAR": "pv", "SHARED": "project"}); err != nil {
		t.Fatalf("store Set() error = %v", err)
	}

	scripts := process.NewScriptRunner(6200, 10)
	scripts.SetProjectEnvResolver(h.app.projectEnvStore.ForRepo)
	ws := &data.Workspace{Name: "ws", Repo: repo, Root: t.TempDir(), Env: map[string]string{"SHARED": "ws"}}

	env, err := scripts.BuildSessionEnv(ws)
	if err != nil {
		t.Fatalf("BuildSessionEnv() error = %v", err)
	}
	m := map[string]string{}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	if m["AMUX_PORT"] == "" || m["AMUX_PORT_RANGE"] == "" || m["AMUX_WORKSPACE_ROOT"] != ws.Root {
		t.Fatalf("injected AMUX_* vars missing: %v", m)
	}
	if m["PROJ_VAR"] != "pv" {
		t.Fatalf("project store value missing: %v", m)
	}
	if _, ok := m["REPO_ONLY"]; ok {
		t.Fatalf("repo env reached a session: %v", m)
	}
	if m["SHARED"] != "ws" {
		t.Fatalf("SHARED = %q, want ws (project < ws)", m["SHARED"])
	}
}
