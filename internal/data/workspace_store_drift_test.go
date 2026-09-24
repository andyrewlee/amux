package data

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// driftFixture builds a workspace whose ID flips between the unresolved and
// resolved normalization forms when the root dir appears/disappears:
// root lives under a symlink (link -> target), so while link/feat is missing
// NormalizePath keeps the link form and once it exists EvalSymlinks resolves
// it to target/feat. Linux CI has no symlinked TMPDIR, so the symlink is
// made explicit here.
type driftFixture struct {
	t      *testing.T
	store  *WorkspaceStore
	target string
	link   string
	ws     *Workspace
}

func newDriftFixture(t *testing.T) *driftFixture {
	t.Helper()
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.MkdirAll(filepath.Join(target, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	ws := NewWorkspace("feat", "feat", "main",
		filepath.Join(target, "repo"), filepath.Join(link, "feat"))
	ws.Created = time.Now()
	return &driftFixture{
		t:      t,
		store:  NewWorkspaceStore(filepath.Join(base, "store")),
		target: target,
		link:   link,
		ws:     ws,
	}
}

func (f *driftFixture) createRoot() {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Join(f.link, "feat"), 0o755); err != nil {
		f.t.Fatal(err)
	}
}

func (f *driftFixture) removeRoot() {
	f.t.Helper()
	if err := os.RemoveAll(filepath.Join(f.target, "feat")); err != nil {
		f.t.Fatal(err)
	}
}

// recordIDs lists the metadata record dirs actually on disk (excluding lock
// files), which is what "exactly one record" means.
func (f *driftFixture) recordIDs() []string {
	f.t.Helper()
	entries, err := os.ReadDir(f.store.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		f.t.Fatal(err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	return ids
}

func TestSameStoredWorkspaceIdentity_CanonicalMatch(t *testing.T) {
	f := newDriftFixture(t)
	if err := f.store.Save(f.ws); err != nil {
		t.Fatal(err)
	}
	id := f.ws.MetadataID()
	if !f.store.sameStoredWorkspaceIdentity(id, f.ws) {
		t.Fatal("same record id + same ws should match")
	}
}

func TestSameStoredWorkspaceIdentity_DriftedCandidate(t *testing.T) {
	f := newDriftFixture(t)
	f.createRoot()
	if err := f.store.Save(f.ws); err != nil {
		t.Fatal(err)
	}
	resolvedID := f.ws.MetadataID()
	f.removeRoot()
	// Plan 042: ID() is the persisted store key — stable across path-existence
	// flips. The drift the guard exists for now lives in ComputedID.
	if f.ws.ID() != resolvedID {
		t.Fatal("ID() must stay pinned to the persisted key after root removal")
	}
	if f.ws.ComputedID() == resolvedID {
		t.Fatal("expected ComputedID drift after root removal")
	}
	if !f.store.sameStoredWorkspaceIdentity(resolvedID, f.ws) {
		t.Fatal("stored resolved-form key should still match the same ws after drift")
	}
}

func TestSameStoredWorkspaceIdentity_NoMatchCases(t *testing.T) {
	f := newDriftFixture(t)
	if err := f.store.Save(f.ws); err != nil {
		t.Fatal(err)
	}
	id := f.ws.MetadataID()

	other := NewWorkspace("other", "other", "main", f.ws.Repo, filepath.Join(f.link, "other"))
	other.Created = time.Now()
	if f.store.sameStoredWorkspaceIdentity(id, other) {
		t.Fatal("different root must not match")
	}
	repoMoved := *f.ws
	repoMoved.Repo = filepath.Join(f.target, "elsewhere")
	if f.store.sameStoredWorkspaceIdentity(id, &repoMoved) {
		t.Fatal("different repo must not match")
	}
	if f.store.sameStoredWorkspaceIdentity(WorkspaceID("deadbeef"), f.ws) {
		t.Fatal("id that is no path-hash of this identity must not match")
	}
	// Legacy record: same repo+root stored under a key that isn't a drift
	// variant of the identity — the guard must miss so Save migrates it.
	legacy := NewWorkspace("legacy", "legacy", "main", f.ws.Repo, f.ws.Root)
	legacy.Created = time.Now()
	legacyID := WorkspaceID("legacykey123")
	if f.store.sameStoredWorkspaceIdentity(legacyID, legacy) {
		t.Fatal("non-hash key must not match even for identical paths")
	}
}

func TestSave_DriftKeepsSingleRecord(t *testing.T) {
	f := newDriftFixture(t)
	// Save unresolved-form key (root missing), then the root appears. ID()
	// stays pinned to the persisted key; ComputedID drifts to the resolved
	// form — which is what legacy artifacts (session names/tags) may carry.
	if err := f.store.Save(f.ws); err != nil {
		t.Fatal(err)
	}
	firstID := f.ws.MetadataID()
	f.createRoot()
	if f.ws.ID() != firstID {
		t.Fatal("ID() must stay pinned to the persisted key after root creation")
	}
	if f.ws.ComputedID() == firstID {
		t.Fatal("expected ComputedID drift after root creation")
	}
	f.ws.Env["K"] = "v"
	if err := f.store.Save(f.ws); err != nil {
		t.Fatal(err)
	}
	if ids := f.recordIDs(); len(ids) != 1 || ids[0] != string(firstID) {
		t.Fatalf("records = %v, want exactly [%s]", ids, firstID)
	}
	loaded, err := f.store.Load(f.ws.MetadataID())
	if err != nil || loaded.Env["K"] != "v" {
		t.Fatalf("load under MetadataID: ws=%+v err=%v", loaded, err)
	}
	f.removeRoot()
	if f.ws.ID() != firstID {
		t.Fatal("ID() must stay pinned to the persisted key after root removal")
	}
	if f.ws.ComputedID() != firstID {
		t.Fatal("expected ComputedID to return to the unresolved form")
	}
}

// TestSave_ResolvedKeySurvivesRootRemoval covers the other drift direction:
// the record was persisted under the resolved-form key, then the root
// vanishes and ComputedID flips to the unresolved form. The persisted key is
// the identity, so re-saving under drift must keep exactly that record.
func TestSave_ResolvedKeySurvivesRootRemoval(t *testing.T) {
	f := newDriftFixture(t)
	f.createRoot()
	if err := f.store.Save(f.ws); err != nil {
		t.Fatal(err)
	}
	resolvedID := f.ws.MetadataID()
	f.removeRoot()
	if f.ws.ID() != resolvedID {
		t.Fatal("ID() must stay pinned to the persisted key after root removal")
	}
	if f.ws.ComputedID() == resolvedID {
		t.Fatal("expected ComputedID drift after root removal")
	}
	if err := f.store.Save(f.ws); err != nil {
		t.Fatal(err)
	}
	if ids := f.recordIDs(); len(ids) != 1 || ids[0] != string(resolvedID) {
		t.Fatalf("records = %v, want exactly [%s]", ids, resolvedID)
	}
}

func TestSave_DriftedRecord_MutationsStaySingle(t *testing.T) {
	f := newDriftFixture(t)
	if err := f.store.Save(f.ws); err != nil {
		t.Fatal(err)
	}
	id := f.ws.MetadataID()
	f.createRoot() // ws.ID() now drifts away from the stored key
	if err := f.store.SetEnv(id, map[string]string{"A": "1"}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetScripts(id, ScriptsConfig{Run: "make run"}, "nonconcurrent"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Rename(id, "renamed"); err != nil {
		t.Fatal(err)
	}
	if ids := f.recordIDs(); len(ids) != 1 || ids[0] != string(id) {
		t.Fatalf("records = %v, want exactly [%s]", ids, id)
	}
	loaded, err := f.store.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "renamed" || loaded.Env["A"] != "1" {
		t.Fatalf("mutations did not land on the drifted record: %+v", loaded)
	}
}

func TestSave_GenuinelyDifferentWorkspaceLeavesRecordAlone(t *testing.T) {
	f := newDriftFixture(t)
	if err := f.store.Save(f.ws); err != nil {
		t.Fatal(err)
	}
	other := NewWorkspace("other", "other", "main", f.ws.Repo, filepath.Join(f.target, "other"))
	other.Created = time.Now()
	if err := f.store.Save(other); err != nil {
		t.Fatal(err)
	}
	if ids := f.recordIDs(); len(ids) != 2 {
		t.Fatalf("records = %v, want 2 distinct records", ids)
	}
}

// TestID_UnsavedWorkspaceUsesComputedForm pins the fallback: before the first
// Save there is no store key, so ID() is the path-derived hash — the same
// value ComputedID reports. After Save, ID() freezes at the minted key.
func TestID_UnsavedWorkspaceUsesComputedForm(t *testing.T) {
	f := newDriftFixture(t)
	if f.ws.ID() != f.ws.ComputedID() {
		t.Fatal("unsaved workspace ID() must equal ComputedID()")
	}
	if err := f.store.Save(f.ws); err != nil {
		t.Fatal(err)
	}
	minted := f.ws.ID()
	f.createRoot() // path identity drifts; persisted identity must not
	if f.ws.ID() != minted {
		t.Fatal("ID() changed after save — identity must be the persisted key")
	}
}
