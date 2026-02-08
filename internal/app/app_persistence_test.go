package app

import (
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/center"
)

func TestPersistAllWorkspacesNowSavesExplicitlyEmptyTabs(t *testing.T) {
	tmp := t.TempDir()
	store := data.NewWorkspaceStore(filepath.Join(tmp, "workspaces-metadata"))
	workspaceService := newWorkspaceService(nil, store, nil, "")

	repo := t.TempDir()
	wsRoot := filepath.Join(tmp, "workspaces", "project", "feature")
	ws := data.NewWorkspace("feature", "feature", "HEAD", repo, wsRoot)
	ws.OpenTabs = []data.TabInfo{
		{
			Assistant:   "claude",
			Name:        "Claude",
			SessionName: "amux-session",
			Status:      "detached",
		},
	}
	ws.ActiveTabIndex = 0
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	project := data.NewProject(repo)
	project.Workspaces = []data.Workspace{*ws}

	centerModel := center.New(nil)
	centerModel.SetWorkspace(ws)
	centerModel.AddTab(&center.Tab{
		ID:          center.TabID("tab-1"),
		Name:        "Claude",
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "amux-session",
		Detached:    true,
	})
	_ = centerModel.CloseActiveTab()

	app := &App{
		workspaceService: workspaceService,
		center:           centerModel,
		projects:         []data.Project{*project},
		dirtyWorkspaces:  make(map[string]bool),
	}

	app.persistAllWorkspacesNow()

	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.OpenTabs) != 0 {
		t.Fatalf("expected empty persisted tabs after closing last tab, got %d", len(loaded.OpenTabs))
	}
}

func TestPersistWorkspaceTabsInitializesDirtyMap(t *testing.T) {
	app := &App{}

	cmd := app.persistWorkspaceTabs("ws-1")
	if cmd == nil {
		t.Fatalf("expected debounce cmd")
	}
	if app.dirtyWorkspaces == nil || !app.dirtyWorkspaces["ws-1"] {
		t.Fatalf("expected workspace to be marked dirty")
	}
}

func TestHandlePersistDebounceSkipsWhenPersistenceDependenciesMissing(t *testing.T) {
	app := &App{
		dirtyWorkspaces: map[string]bool{"ws-1": true},
		persistToken:    1,
	}

	if cmd := app.handlePersistDebounce(persistDebounceMsg{token: 1}); cmd != nil {
		t.Fatalf("expected nil cmd when center/workspaceService are unavailable")
	}
}
