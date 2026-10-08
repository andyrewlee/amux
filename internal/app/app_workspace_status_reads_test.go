package app

import (
	"sync/atomic"
	"testing"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// countingEnvStore/countingScriptStore record ForRepo calls so tests can
// prove the on-loop assembly performs no file reads — every store read must
// arrive prefetched on workspaceStatusReads.
type countingEnvStore struct {
	calls atomic.Int32
	env   map[string]string
}

func (s *countingEnvStore) ForRepo(string) map[string]string {
	s.calls.Add(1)
	return s.env
}

func (s *countingEnvStore) Set(string, map[string]string) error { return nil }

type countingScriptStore struct {
	calls atomic.Int32
	cfg   data.ScriptsConfig
}

func (s *countingScriptStore) ForRepo(string) data.ScriptsConfig {
	s.calls.Add(1)
	return s.cfg
}

// TestBuildWorkspaceStatus_PerformsNoStoreReads proves the on-loop assembly
// is pure: with counting fakes installed, buildWorkspaceStatus consumes only
// the prefetched reads and never touches the stores — the file I/O lives in
// fetchWorkspaceStatusReads inside the open cmd.
func TestBuildWorkspaceStatus_PerformsNoStoreReads(t *testing.T) {
	envStore := &countingEnvStore{env: map[string]string{"PROJ_KEY": "p"}}
	scriptStore := &countingScriptStore{cfg: data.ScriptsConfig{Run: "echo run"}}
	app := &App{
		config:             &config.Config{PortRangeSize: 10},
		toast:              common.NewToastModel(),
		workspaceService:   workspacesvc.New(nil, nil, process.NewScriptRunner(6200, 10), ""),
		projectEnvStore:    envStore,
		projectScriptStore: scriptStore,
	}
	ws := &data.Workspace{Name: "ws", Repo: t.TempDir(), Root: t.TempDir()}

	// The fetch is the one place store calls happen — it rides the open cmd.
	reads := app.fetchWorkspaceStatusReads(ws)
	if envStore.calls.Load() != 1 || scriptStore.calls.Load() != 1 {
		t.Fatalf("fetch did not read each store once: env=%d scripts=%d",
			envStore.calls.Load(), scriptStore.calls.Load())
	}

	st := app.buildWorkspaceStatus(ws, reads)
	if envStore.calls.Load() != 1 || scriptStore.calls.Load() != 1 {
		t.Fatalf("on-loop assembly hit the stores: env=%d scripts=%d",
			envStore.calls.Load(), scriptStore.calls.Load())
	}
	if st.envKeySource["PROJ_KEY"] != "project" {
		t.Fatalf("prefetched project env did not reach the snapshot: %v", st.envKeySource)
	}
	if st.scriptSources[process.ScriptRun] != "project" {
		t.Fatalf("prefetched project script did not reach the snapshot: %v", st.scriptSources)
	}
}

// TestHandleShowWorkspaceStatus_ReadyMsgCarriesReads proves the open cmd
// bundles the file reads onto the ready msg — the on-loop handler consumes
// them verbatim.
func TestHandleShowWorkspaceStatus_ReadyMsgCarriesReads(t *testing.T) {
	repo := t.TempDir()
	envStore := &countingEnvStore{env: map[string]string{"PROJ_KEY": "p"}}
	scriptStore := &countingScriptStore{cfg: data.ScriptsConfig{OnDone: "echo done"}}
	app := &App{
		config:             &config.Config{PortRangeSize: 10},
		toast:              common.NewToastModel(),
		workspaceService:   workspacesvc.New(nil, nil, process.NewScriptRunner(6200, 10), ""),
		projectEnvStore:    envStore,
		projectScriptStore: scriptStore,
	}
	ws := &data.Workspace{Name: "ws", Repo: repo, Root: t.TempDir()}

	cmd := app.handleShowWorkspaceStatus(messages.ShowWorkspaceStatus{Workspace: ws})
	if cmd == nil {
		t.Fatal("expected the status open to return a fetch cmd")
	}
	ready, ok := cmd().(workspaceStatusReadyMsg)
	if !ok {
		t.Fatalf("fetch cmd emitted %T, want workspaceStatusReadyMsg", cmd())
	}
	if ready.reads.projectEnv["PROJ_KEY"] != "p" || ready.reads.projectScripts.OnDone != "echo done" {
		t.Fatalf("ready msg missing prefetched reads: %+v", ready.reads)
	}
}
