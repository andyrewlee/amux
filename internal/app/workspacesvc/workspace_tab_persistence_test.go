package workspacesvc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil"
)

// newTabPersistHarness builds a service backed by a real store with one
// saved workspace, for exercising SaveWorkspaceTabs' narrow-write and
// sequence semantics.
func newTabPersistHarness(t *testing.T) (*Service, *data.Workspace, *data.WorkspaceStore) {
	t.Helper()
	repo := t.TempDir()
	managed := t.TempDir()
	store := data.NewWorkspaceStore(t.TempDir())
	ws := data.NewWorkspace("feat", "feat", "main", repo, managed+"/repo/feat")
	if err := store.Save(ws); err != nil {
		t.Fatalf("seed Save() error = %v", err)
	}
	svc := New(nil, store, nil, managed)
	return svc, ws, store
}

func tabsNamed(names ...string) []data.TabInfo {
	tabs := make([]data.TabInfo, len(names))
	for i, n := range names {
		tabs[i] = data.TabInfo{Name: n}
	}
	return tabs
}

// TestSaveWorkspaceTabs_NarrowWritePreservesOtherFields is the plan-039 core:
// a queued tab capture must not resurrect stale Env/Scripts/flags over a
// concurrent field update that landed after the snapshot was taken.
func TestSaveWorkspaceTabs_NarrowWritePreservesOtherFields(t *testing.T) {
	svc, ws, store := newTabPersistHarness(t)

	// A concurrent writer changes Env on the committed record AFTER the
	// caller's ws snapshot was taken (the snapshot still carries no Env).
	if err := store.SetEnv(ws.MetadataID(), map[string]string{"LIVE": "yes"}); err != nil {
		t.Fatalf("SetEnv() error = %v", err)
	}
	if err := store.Rename(ws.MetadataID(), "live-name"); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}

	committed, err := svc.SaveWorkspaceTabs(ws, 1, tabsNamed("a", "b"), 1)
	if err != nil {
		t.Fatalf("SaveWorkspaceTabs() error = %v", err)
	}
	if !committed {
		t.Fatal("first tab write should commit")
	}
	loaded, err := store.Load(ws.MetadataID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.OpenTabs) != 2 || loaded.ActiveTabIndex != 1 {
		t.Fatalf("tabs not persisted: OpenTabs=%v Active=%d", loaded.OpenTabs, loaded.ActiveTabIndex)
	}
	if loaded.Env["LIVE"] != "yes" || loaded.Name != "live-name" {
		t.Fatalf("stale snapshot clobbered concurrent fields: Name=%q Env=%v", loaded.Name, loaded.Env)
	}
}

// TestSaveWorkspaceTabs_OlderSequenceLoses verifies a stale capture that
// executes after a newer one is dropped instead of reverting the record.
func TestSaveWorkspaceTabs_OlderSequenceLoses(t *testing.T) {
	svc, ws, store := newTabPersistHarness(t)

	committed, err := svc.SaveWorkspaceTabs(ws, 5, tabsNamed("new"), 0)
	if err != nil || !committed {
		t.Fatalf("seq-5 write committed=%v err=%v, want committed", committed, err)
	}
	// Command reversal: an older capture arrives after the newer commit.
	committed, err = svc.SaveWorkspaceTabs(ws, 3, tabsNamed("old"), 0)
	if err != nil {
		t.Fatalf("seq-3 write error = %v", err)
	}
	if committed {
		t.Fatal("stale sequence committed over a newer one")
	}
	loaded, err := store.Load(ws.MetadataID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.OpenTabs) != 1 || loaded.OpenTabs[0].Name != "new" {
		t.Fatalf("stale sequence reverted tabs to %v", loaded.OpenTabs)
	}
}

// TestSaveWorkspaceTabs_FailedWriteDoesNotCommitSequence verifies a real
// store failure leaves the sequence uncommitted so the app's re-dirty retry
// can still persist that capture.
func TestSaveWorkspaceTabs_FailedWriteDoesNotCommitSequence(t *testing.T) {
	store := data.NewWorkspaceStore(t.TempDir())
	ws := data.NewWorkspace("feat", "feat", "main", t.TempDir(), t.TempDir()+"/feat")
	if err := store.Save(ws); err != nil {
		t.Fatalf("seed Save() error = %v", err)
	}
	fake := &testutil.FakeWorkspaceStore{}
	fake.UpdateFunc = func(id data.WorkspaceID, fn func(ws *data.Workspace) (bool, error)) error {
		return errors.New("injected tab save failure")
	}
	svc := New(nil, fake, nil, t.TempDir())

	if _, err := svc.SaveWorkspaceTabs(ws, 7, tabsNamed("x"), 0); err == nil {
		t.Fatal("injected store failure must surface")
	}
	// Sequence 7 was not committed: a real store accepts the retry.
	svc.store = store
	committed, err := svc.SaveWorkspaceTabs(ws, 7, tabsNamed("x"), 0)
	if err != nil || !committed {
		t.Fatalf("retry after failure committed=%v err=%v, want committed", committed, err)
	}
}

// TestSaveWorkspaceTabs_MutationInFlightSkips verifies the atomic guard: a
// workspace mid-mutation reports a benign uncommitted skip and the record is
// untouched — the lifecycle's own requeue path owns the retry.
func TestSaveWorkspaceTabs_MutationInFlightSkips(t *testing.T) {
	svc, ws, store := newTabPersistHarness(t)
	svc.mutationInFlight = func(checked *data.Workspace) bool {
		return checked.Root == ws.Root
	}
	committed, err := svc.SaveWorkspaceTabs(ws, 1, tabsNamed("x"), 0)
	if err != nil {
		t.Fatalf("in-flight skip must be benign, got %v", err)
	}
	if committed {
		t.Fatal("in-flight skip reported a write")
	}
	loaded, err := store.Load(ws.MetadataID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.OpenTabs) != 0 {
		t.Fatalf("skipped write still persisted tabs %v", loaded.OpenTabs)
	}
	// Once the mutation resolves the same sequence commits normally.
	svc.mutationInFlight = nil
	committed, err = svc.SaveWorkspaceTabs(ws, 1, tabsNamed("x"), 0)
	if err != nil || !committed {
		t.Fatalf("post-mutation retry committed=%v err=%v", committed, err)
	}
}

// TestSaveWorkspaceTabs_CreatesMissingRecord verifies the not-exist fallback:
// a workspace that exists only in memory gets a whole-record create, not an
// error — Update deliberately never creates.
func TestSaveWorkspaceTabs_CreatesMissingRecord(t *testing.T) {
	store := data.NewWorkspaceStore(t.TempDir())
	svc := New(nil, store, nil, t.TempDir())
	ws := data.NewWorkspace("ghost", "feat", "main", t.TempDir(), t.TempDir()+"/feat")

	committed, err := svc.SaveWorkspaceTabs(ws, 1, tabsNamed("a"), 0)
	if err != nil {
		t.Fatalf("SaveWorkspaceTabs() error = %v", err)
	}
	if !committed {
		t.Fatal("missing-record create should report a write")
	}
	loaded, err := store.Load(ws.MetadataID())
	if err != nil || loaded == nil {
		t.Fatalf("record was not created: %v", err)
	}
	if loaded.Name != "ghost" || len(loaded.OpenTabs) != 1 {
		t.Fatalf("created record missing fields: Name=%q tabs=%v", loaded.Name, loaded.OpenTabs)
	}
}

// TestSaveWorkspaceTabs_SequencesArePerWorkspace verifies the ordering key
// is scoped per workspace ID — one workspace's high sequence must not gate
// another's first write.
func TestSaveWorkspaceTabs_SequencesArePerWorkspace(t *testing.T) {
	svc, ws, _ := newTabPersistHarness(t)
	other := data.NewWorkspace("other", "other", "main", ws.Repo, ws.Repo+"-other/feat")
	if err := svc.store.Save(other); err != nil {
		t.Fatalf("Save(other) error = %v", err)
	}
	if _, err := svc.SaveWorkspaceTabs(ws, 9, tabsNamed("hi"), 0); err != nil {
		t.Fatalf("ws write: %v", err)
	}
	committed, err := svc.SaveWorkspaceTabs(other, 1, tabsNamed("lo"), 0)
	if err != nil || !committed {
		t.Fatalf("other workspace's seq-1 write gated by ws seq-9: committed=%v err=%v", committed, err)
	}
}

// TestShelveIntent_PreservesConcurrentFieldWrites covers the lifecycle side
// of the plan: the intent write is a field transaction, so fields another
// writer committed after the caller's snapshot survive the shelve.
func TestShelveIntent_PreservesConcurrentFieldWrites(t *testing.T) {
	svc, project, ws, store := newShelveHarness(t, &testutil.FakeGitOps{
		RemoveWorkspaceFunc: func(_, _ string) error { return nil },
	})
	// Concurrent field update lands on the committed record after the
	// caller's ws snapshot was taken — the snapshot still has no Env.
	if err := store.SetEnv(ws.MetadataID(), map[string]string{"LIVE": "1"}); err != nil {
		t.Fatalf("SetEnv() error = %v", err)
	}
	if err := store.Rename(ws.MetadataID(), "live-name"); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	msg := svc.ShelveWorkspace(project, ws)()
	if _, ok := msg.(messages.WorkspaceShelved); !ok {
		t.Fatalf("shelve = %T (%v), want WorkspaceShelved", msg, msg)
	}
	loaded, err := store.Load(ws.MetadataID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !loaded.Shelved || !loaded.Archived {
		t.Fatalf("intent flags not persisted: Shelved=%v Archived=%v", loaded.Shelved, loaded.Archived)
	}
	if loaded.Env["LIVE"] != "1" || loaded.Name != "live-name" {
		t.Fatalf("shelve intent resurrected stale fields: Name=%q Env=%v", loaded.Name, loaded.Env)
	}
}

// TestRestoreUnarchive_PreservesConcurrentFieldWrites is the unarchive
// counterpart: the restore's field transaction must not revert a rename
// that committed while the restore was in flight.
func TestRestoreUnarchive_PreservesConcurrentFieldWrites(t *testing.T) {
	svc, project, ws, store := newShelveHarness(t, &testutil.FakeGitOps{
		CreateWorkspaceFunc: func(_, workspacePath, _, _ string) error {
			return os.MkdirAll(filepath.Join(workspacePath, ".git"), 0o755)
		},
	})
	ws.Archived = true
	ws.Shelved = true
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.Rename(ws.MetadataID(), "live-name"); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	msg := svc.RestoreWorkspace(project, ws)()
	if _, ok := msg.(messages.WorkspaceRestored); !ok {
		t.Fatalf("restore = %T (%v), want WorkspaceRestored", msg, msg)
	}
	loaded, err := store.Load(ws.MetadataID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Archived || loaded.Shelved {
		t.Fatalf("restore left flags set: Archived=%v Shelved=%v", loaded.Archived, loaded.Shelved)
	}
	if loaded.Name != "live-name" {
		t.Fatalf("restore reverted concurrent rename: Name=%q", loaded.Name)
	}
}
