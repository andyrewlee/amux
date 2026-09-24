package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// TestOverlayChainConsumeOrder pins the pre-switch consume order across the
// bespoke overlays: with the workspace env editor AND the project env editor
// both somehow live, the workspace editor (earlier in the chain) eats the
// keystroke — the same winner the old sequential checks produced.
func TestOverlayChainConsumeOrder(t *testing.T) {
	ws := &data.Workspace{
		Name: "feature",
		Repo: "/repo/primary",
		Root: "/repo/primary/ws",
		Env:  map[string]string{"K": "v"},
	}
	h, _, _ := newEnvTestHarness(t, ws)
	store := data.NewProjectEnvStore(t.TempDir())
	h.app.projectEnvStore = store
	if err := store.Set(ws.Repo, map[string]string{"K": "p"}); err != nil {
		t.Fatal(err)
	}

	// The overlay arbiter (plan 126) makes "both live" unreachable through
	// the open handlers — the second open defers until the first closes. The
	// consume-order invariant is still worth pinning as a defensive property,
	// so the fixture constructs the stacked state directly.
	h.app.overlays.envWorkspace = ws
	h.app.overlays.env = common.NewEnvDialog(ws.Env)
	h.app.overlays.env.Show()
	h.app.overlays.projectEnvRepo = ws.Repo
	h.app.overlays.projectEnv = common.NewEnvDialog(store.ForRepo(ws.Repo))
	h.app.overlays.projectEnv.Show()
	if h.app.overlays.env == nil || h.app.overlays.projectEnv == nil {
		t.Fatal("fixture must have both env overlays live")
	}

	var cmds []tea.Cmd
	if _, consumed := h.app.handlePreSwitchInput(tea.KeyPressMsg{Code: 'X', Text: "X"}, &cmds); !consumed {
		t.Fatal("expected the overlay chain to consume the keypress")
	}
	if got := h.app.overlays.env.Env()["K"]; got != "vX" {
		t.Fatalf("workspace env dialog got %q, want %q — earlier slot must win", got, "vX")
	}
	if got := h.app.overlays.projectEnv.Env()["K"]; got != "p" {
		t.Fatalf("project env dialog got %q, want %q — later slot must not see the key", got, "p")
	}
}

// TestEnvDialogResultRoutesByScope pins scope-based routing: a
// workspace-scoped result reaches the workspace handler even while the
// project overlay's context is still populated — the nil-check routing this
// replaces would have misrouted it to the project handler.
func TestEnvDialogResultRoutesByScope(t *testing.T) {
	ws := &data.Workspace{
		Name: "feature",
		Repo: "/repo/primary",
		Root: "/repo/primary/ws",
		Env:  map[string]string{"K": "v"},
	}
	h, store, _ := newEnvTestHarness(t, ws)
	projStore := data.NewProjectEnvStore(t.TempDir())
	h.app.projectEnvStore = projStore
	if err := projStore.Set(ws.Repo, map[string]string{"P": "1"}); err != nil {
		t.Fatal(err)
	}

	h.app.handleShowWorkspaceEnvDialog(messages.ShowWorkspaceEnvDialog{Workspace: ws})
	h.app.overlays.env.Update(tea.KeyPressMsg{Code: 'X', Text: "X"})
	// Simulate the lingering state that made nil-check routing wrong: a
	// project-env context still populated from an earlier open.
	h.app.overlays.projectEnv = common.NewEnvDialog(map[string]string{"P": "1"})
	h.app.overlays.projectEnvRepo = ws.Repo

	var cmds []tea.Cmd
	h.app.updateDialogShowMsg(common.EnvDialogResult{Scope: common.EnvScopeWorkspace}, &cmds)

	loaded, err := store.Load(ws.MetadataID())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Env["K"] != "vX" {
		t.Fatalf("workspace env K = %q, want %q — workspace handler must persist the edit", loaded.Env["K"], "vX")
	}
	if h.app.overlays.envWorkspace != nil {
		t.Fatal("workspace env context must be consumed by the workspace handler")
	}
	if h.app.overlays.projectEnvRepo == "" {
		t.Fatal("project env context must NOT be consumed by a workspace-scoped result")
	}
	if got := projStore.ForRepo(ws.Repo); got["P"] != "1" {
		t.Fatalf("project store mutated by workspace-scoped result: %v", got)
	}
}

// TestEnvDialogResultProjectScopeRoutesToProjectHandler covers the reverse
// direction: the project-scoped result reaches the project handler even while
// the workspace editor is live.
func TestEnvDialogResultProjectScopeRoutesToProjectHandler(t *testing.T) {
	ws := &data.Workspace{
		Name: "feature",
		Repo: "/repo/primary",
		Root: "/repo/primary/ws",
		Env:  map[string]string{"K": "v"},
	}
	h, store, _ := newEnvTestHarness(t, ws)
	projStore := data.NewProjectEnvStore(t.TempDir())
	h.app.projectEnvStore = projStore
	if err := projStore.Set(ws.Repo, map[string]string{"P": "1"}); err != nil {
		t.Fatal(err)
	}

	h.app.handleShowProjectEnvDialog(messages.ShowProjectEnvDialog{Workspace: ws})
	h.app.overlays.projectEnv.Update(tea.KeyPressMsg{Code: 'X', Text: "X"})
	// A live workspace editor must not divert the project-scoped result.
	h.app.overlays.env = common.NewEnvDialog(map[string]string{"K": "v"})
	h.app.overlays.envWorkspace = ws

	var cmds []tea.Cmd
	h.app.updateDialogShowMsg(common.EnvDialogResult{Scope: common.EnvScopeProject}, &cmds)

	if got := projStore.ForRepo(ws.Repo); got["P"] != "1X" {
		t.Fatalf("project env P = %q, want %q — project handler must persist the edit", got["P"], "1X")
	}
	if h.app.overlays.projectEnvRepo != "" || h.app.overlays.projectEnv != nil {
		t.Fatal("project env context must be consumed by the project handler")
	}
	if h.app.overlays.envWorkspace == nil {
		t.Fatal("workspace env context must NOT be consumed by a project-scoped result")
	}
	loaded, err := store.Load(ws.MetadataID())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Env["K"] != "v" {
		t.Fatalf("workspace env K = %q, want unchanged %q", loaded.Env["K"], "v")
	}
}
