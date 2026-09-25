package app

import (
	"errors"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/testutil/tmuxops"
	"github.com/andyrewlee/amux/internal/tmux"
)

// These tests exercise the producer CLOSURES in app_tmux_discover.go and
// app_input_messages_tmux.go — the emitted message type and fields on
// success, error, and empty paths. The result HANDLERS are covered
// elsewhere; the emit logic itself was the coverage gap.

func producerWorkspace() *data.Workspace {
	return data.NewWorkspace("feat", "feat", "main", "/repo", "/repo/managed/feat")
}

func TestDiscoverWorkspaceTabsFromTmux_Guards(t *testing.T) {
	app := &App{tmuxAvailable: true, tmuxService: &tmuxops.FakeTmuxOps{}}
	if cmd := app.discoverWorkspaceTabsFromTmux(nil); cmd != nil {
		t.Fatal("nil workspace should produce nil Cmd")
	}
	app.tmuxAvailable = false
	if cmd := app.discoverWorkspaceTabsFromTmux(producerWorkspace()); cmd != nil {
		t.Fatal("tmux-unavailable should produce nil Cmd")
	}
}

func TestDiscoverWorkspaceTabsFromTmux_NilService(t *testing.T) {
	app := &App{tmuxAvailable: true}
	cmd := app.discoverWorkspaceTabsFromTmux(producerWorkspace())
	if cmd == nil {
		t.Fatal("expected a producer Cmd")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("nil tmuxService should emit nil, got %T", msg)
	}
}

func TestDiscoverWorkspaceTabsFromTmux_Error(t *testing.T) {
	app := &App{
		tmuxAvailable: true,
		tmuxService: &tmuxops.FakeTmuxOps{
			SessionsWithTagsFunc: func(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
				return nil, errors.New("tmux down")
			},
		},
	}
	cmd := app.discoverWorkspaceTabsFromTmux(producerWorkspace())
	if msg := cmd(); msg != nil {
		t.Fatalf("SessionsWithTags error should emit nil, got %T", msg)
	}
}

func TestDiscoverWorkspaceTabsFromTmux_Success(t *testing.T) {
	ws := producerWorkspace()
	// An already-known session must be filtered out of the result.
	ws.OpenTabs = []data.TabInfo{{SessionName: "amux-known"}}
	metaCalls := 0
	app := &App{
		tmuxAvailable: true,
		tmuxService: &tmuxops.FakeTmuxOps{
			SessionsWithTagsFunc: func(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
				wsID := string(ws.ID())
				return []tmux.SessionTagValues{
					{Name: "amux-known", Tags: map[string]string{"@amux_workspace": wsID, "@amux_assistant": "claude"}},
					{Name: "amux-new-b", Tags: map[string]string{"@amux_workspace": wsID, "@amux_assistant": "codex", "@amux_created_at": "200"}},
					{Name: "amux-new-a", Tags: map[string]string{"@amux_workspace": wsID, "@amux_assistant": "claude", "@amux_created_at": "100"}},
					{Name: "amux-foreign", Tags: map[string]string{"@amux_workspace": "other-ws", "@amux_assistant": "claude"}},
				}, nil
			},
			AllSessionMetaFunc: func(tmux.Options) (map[string]tmux.SessionMeta, error) {
				metaCalls++
				return map[string]tmux.SessionMeta{}, nil
			},
		},
	}
	msg := app.discoverWorkspaceTabsFromTmux(ws)()
	res, ok := msg.(tmuxTabsDiscoverResult)
	if !ok {
		t.Fatalf("emitted %T, want tmuxTabsDiscoverResult", msg)
	}
	if res.WorkspaceID != string(ws.ID()) {
		t.Fatalf("WorkspaceID = %q, want %q", res.WorkspaceID, ws.ID())
	}
	if len(res.Tabs) != 2 {
		t.Fatalf("Tabs = %d, want 2 (known session filtered)", len(res.Tabs))
	}
	// createdAt ascending: amux-new-a (100) before amux-new-b (200).
	if res.Tabs[0].SessionName != "amux-new-a" || res.Tabs[1].SessionName != "amux-new-b" {
		t.Fatalf("Tabs order = [%s %s], want createdAt ascending",
			res.Tabs[0].SessionName, res.Tabs[1].SessionName)
	}
	if res.Tabs[0].CreatedAt != 100 || res.Tabs[0].Status != "running" {
		t.Fatalf("tab = %+v, want CreatedAt=100 Status=running", res.Tabs[0])
	}
	// Every row carried @amux_created_at (or was filtered) — the batched meta
	// fetch must not have fired.
	if metaCalls != 0 {
		t.Fatalf("AllSessionMeta calls = %d, want 0 (all rows had created_at)", metaCalls)
	}
}

// TestDiscoverWorkspaceTabsFromTmux_LazyMetaFetch: a row missing
// @amux_created_at triggers exactly one batched AllSessionMeta fetch; rows
// that already carry the tag never consult it.
func TestDiscoverWorkspaceTabsFromTmux_LazyMetaFetch(t *testing.T) {
	ws := producerWorkspace()
	metaCalls := 0
	app := &App{
		tmuxAvailable: true,
		tmuxService: &tmuxops.FakeTmuxOps{
			SessionsWithTagsFunc: func(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
				wsID := string(ws.ID())
				return []tmux.SessionTagValues{
					{Name: "amux-tagged", Tags: map[string]string{"@amux_workspace": wsID, "@amux_created_at": "100"}},
					{Name: "amux-untagged", Tags: map[string]string{"@amux_workspace": wsID}},
					{Name: "amux-untagged-2", Tags: map[string]string{"@amux_workspace": wsID}},
				}, nil
			},
			AllSessionMetaFunc: func(tmux.Options) (map[string]tmux.SessionMeta, error) {
				metaCalls++
				return map[string]tmux.SessionMeta{
					"amux-untagged": {CreatedAt: 50},
				}, nil
			},
		},
	}
	res, ok := app.discoverWorkspaceTabsFromTmux(ws)().(tmuxTabsDiscoverResult)
	if !ok {
		t.Fatal("expected tmuxTabsDiscoverResult")
	}
	if metaCalls != 1 {
		t.Fatalf("AllSessionMeta calls = %d, want exactly 1 lazy fetch", metaCalls)
	}
	gotCreated := map[string]int64{}
	for _, tab := range res.Tabs {
		gotCreated[tab.SessionName] = tab.CreatedAt
	}
	if gotCreated["amux-untagged"] != 50 {
		t.Fatalf("untagged CreatedAt = %d, want 50 from meta", gotCreated["amux-untagged"])
	}
	if gotCreated["amux-tagged"] != 100 {
		t.Fatalf("tagged CreatedAt = %d, want 100 from tag", gotCreated["amux-tagged"])
	}
}

func TestDiscoverSidebarTerminalsFromTmux_Unavailable(t *testing.T) {
	app := &App{tmuxAvailable: false}
	ws := producerWorkspace()
	cmd := app.discoverSidebarTerminalsFromTmux(ws)
	if cmd == nil {
		t.Fatal("tmux-unavailable should still emit a result (empty) Cmd")
	}
	res, ok := cmd().(tmuxSidebarDiscoverResult)
	if !ok {
		t.Fatalf("emitted %T, want tmuxSidebarDiscoverResult", res)
	}
	if res.WorkspaceID != string(ws.ID()) || len(res.Sessions) != 0 {
		t.Fatalf("result = %+v, want empty sessions for the ws", res)
	}
}

func TestDiscoverSidebarTerminalsFromTmux_Error(t *testing.T) {
	ws := producerWorkspace()
	app := &App{
		tmuxAvailable: true,
		tmuxService: &tmuxops.FakeTmuxOps{
			SessionsWithTagsFunc: func(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
				return nil, errors.New("tmux down")
			},
		},
	}
	res, ok := app.discoverSidebarTerminalsFromTmux(ws)().(tmuxSidebarDiscoverResult)
	if !ok {
		t.Fatal("error path must emit tmuxSidebarDiscoverResult")
	}
	if len(res.Sessions) != 0 {
		t.Fatalf("Sessions = %d, want empty on discovery error", len(res.Sessions))
	}
}

func TestDiscoverSidebarTerminalsFromTmux_Success(t *testing.T) {
	ws := producerWorkspace()
	app := &App{
		tmuxAvailable: true,
		tmuxService: &tmuxops.FakeTmuxOps{
			SessionsWithTagsFunc: func(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
				wsID := string(ws.ID())
				return []tmux.SessionTagValues{
					{Name: "amux-term-live", Tags: map[string]string{"@amux_workspace": wsID, "@amux_instance": "inst-1", "@amux_created_at": "50"}},
					{Name: "amux-term-dead", Tags: map[string]string{"@amux_workspace": wsID, "@amux_instance": "inst-1"}},
				}, nil
			},
			AllSessionStatesFunc: func(tmux.Options) (map[string]tmux.SessionState, error) {
				return map[string]tmux.SessionState{
					"amux-term-live": {Exists: true, HasLivePane: true},
					// amux-term-dead absent → filtered out.
				}, nil
			},
			AllSessionMetaFunc: func(tmux.Options) (map[string]tmux.SessionMeta, error) {
				return map[string]tmux.SessionMeta{
					"amux-term-live": {Attached: 0},
				}, nil
			},
		},
	}
	res, ok := app.discoverSidebarTerminalsFromTmux(ws)().(tmuxSidebarDiscoverResult)
	if !ok {
		t.Fatal("expected tmuxSidebarDiscoverResult")
	}
	if len(res.Sessions) != 1 {
		t.Fatalf("Sessions = %d, want 1 (dead session filtered)", len(res.Sessions))
	}
	s := res.Sessions[0]
	if s.Name != "amux-term-live" {
		t.Fatalf("SessionName = %q", s.Name)
	}
}

func TestSyncWorkspaceTabsFromTmux_Guards(t *testing.T) {
	app := &App{tmuxAvailable: true, tmuxService: &tmuxops.FakeTmuxOps{}}
	if cmd := app.syncWorkspaceTabsFromTmux(nil); cmd != nil {
		t.Fatal("nil workspace → nil Cmd")
	}
	ws := producerWorkspace()
	if cmd := app.syncWorkspaceTabsFromTmux(ws); cmd != nil {
		t.Fatal("workspace with no tabs → nil Cmd")
	}
	app.tmuxAvailable = false
	ws.OpenTabs = []data.TabInfo{{SessionName: "amux-s1"}}
	if cmd := app.syncWorkspaceTabsFromTmux(ws); cmd != nil {
		t.Fatal("tmux-unavailable → nil Cmd")
	}
}

func TestSyncWorkspaceTabsFromTmux_Error(t *testing.T) {
	ws := producerWorkspace()
	ws.OpenTabs = []data.TabInfo{{SessionName: "amux-s1", Status: "running"}}
	app := &App{
		tmuxAvailable: true,
		tmuxService: &tmuxops.FakeTmuxOps{
			AllSessionStatesFunc: func(tmux.Options) (map[string]tmux.SessionState, error) {
				return nil, errors.New("tmux down")
			},
		},
	}
	res, ok := app.syncWorkspaceTabsFromTmux(ws)().(tmuxTabsSyncResult)
	if !ok {
		t.Fatal("error path must emit tmuxTabsSyncResult")
	}
	if len(res.Updates) != 0 {
		t.Fatalf("Updates = %d, want none when tmux unresponsive", len(res.Updates))
	}
}

func TestSyncWorkspaceTabsFromTmux_StatusFlips(t *testing.T) {
	ws := producerWorkspace()
	ws.OpenTabs = []data.TabInfo{
		{SessionName: "amux-alive", Status: "running"},
		{SessionName: "amux-dead", Status: "running"},
		{SessionName: "amux-revived", Status: "stopped"},
		{SessionName: "amux-det", Status: "detached"},
		{SessionName: "", Status: "running"}, // no session — skipped
	}
	app := &App{
		tmuxAvailable: true,
		tmuxService: &tmuxops.FakeTmuxOps{
			AllSessionStatesFunc: func(tmux.Options) (map[string]tmux.SessionState, error) {
				return map[string]tmux.SessionState{
					"amux-alive":   {Exists: true, HasLivePane: true},
					"amux-dead":    {Exists: false},
					"amux-revived": {Exists: true, HasLivePane: true},
					// amux-det absent → detached tab reconciles to stopped.
				}, nil
			},
		},
	}
	res, ok := app.syncWorkspaceTabsFromTmux(ws)().(tmuxTabsSyncResult)
	if !ok {
		t.Fatal("expected tmuxTabsSyncResult")
	}
	updates := map[string]tmuxTabStatusUpdate{}
	for _, u := range res.Updates {
		updates[u.SessionName] = u
	}
	if len(updates) != 3 {
		t.Fatalf("Updates = %v, want 3 (dead→stopped, revived→running, detached→stopped)", res.Updates)
	}
	if u := updates["amux-dead"]; u.Status != "stopped" || !u.NotifyStopped {
		t.Fatalf("amux-dead update = %+v, want stopped+notify", u)
	}
	if u := updates["amux-revived"]; u.Status != "running" || u.NotifyStopped {
		t.Fatalf("amux-revived update = %+v, want running, no notify", u)
	}
	if u := updates["amux-det"]; u.Status != "stopped" || !u.NotifyStopped {
		t.Fatalf("amux-det update = %+v, want stopped+notify", u)
	}
	if _, ok := updates["amux-alive"]; ok {
		t.Fatal("amux-alive should produce no update (still running)")
	}
}
