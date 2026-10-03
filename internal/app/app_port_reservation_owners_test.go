package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// shelvedWorkspace builds a workspace record as it appears on a project's
// ShelvedWorkspaces slice: archived + intentionally shelved.
func shelvedWorkspace(name string) *data.Workspace {
	return &data.Workspace{
		Name:     name,
		Repo:     filepath.Join("repo", name),
		Root:     filepath.Join("workspaces", name),
		Branch:   "main",
		Shelved:  true,
		Archived: true,
	}
}

// appWithShelf wraps reclaimableStatusApp with one project that carries a
// shelved workspace alongside its live list.
func appWithShelf(t *testing.T, live []*data.Workspace, shelf *data.Workspace) (*App, *data.PortReservationStore) {
	t.Helper()
	app, resStore, _ := reclaimableStatusApp(t, live)
	for i := range app.projects {
		app.projects[i].ShelvedWorkspaces = append(app.projects[i].ShelvedWorkspaces, *shelf)
	}
	if len(app.projects) == 0 {
		app.projects = append(app.projects, data.Project{
			Name:              "proj",
			Workspaces:        nil,
			ShelvedWorkspaces: []data.Workspace{*shelf},
		})
	}
	return app, resStore
}

// TestPortReservationOwners_ShelfRetainedOnRelease: a shelved record with no
// tmux sessions still owns its port range — the probe counts only the
// genuinely deleted ID and the release deletes only that entry.
func TestPortReservationOwners_ShelfRetainedOnRelease(t *testing.T) {
	shelf := shelvedWorkspace("shelf-ws")
	app, resStore := appWithShelf(t, nil, shelf)
	shelfID := string(shelf.MetadataID())
	mustReserveCleanup(t, resStore, shelfID)
	mustReserveCleanup(t, resStore, "deadbeef89abcdef")

	// The displayed/probed orphan count excludes the shelf.
	ws := &data.Workspace{Name: "ws", Repo: t.TempDir(), Root: t.TempDir(), Branch: "feat"}
	statusInterval(t, app, ws)
	if app.overlays.runOutputReleaseCount != 1 {
		t.Fatalf("release count = %d, want 1 (shelf excluded)", app.overlays.runOutputReleaseCount)
	}

	// Release through the real confirm path: only the true orphan goes.
	cmd := dialogResultReleasePortReservations(app, common.DialogResult{
		ID: DialogReleasePortReservations, Confirmed: true, Value: "1",
	}, dialogContext{portReleaseCount: 1})
	res, ok := cmd().(reservationReleaseResultMsg)
	if !ok {
		t.Fatalf("release cmd emitted %T", cmd())
	}
	if res.err != nil || res.count != 1 {
		t.Fatalf("release = count %d err %v, want 1,nil", res.count, res.err)
	}
	snap, err := resStore.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if _, ok := snap[shelfID]; !ok {
		t.Fatalf("shelf reservation %q was released: %v", shelfID, snap)
	}
	if _, ok := snap["deadbeef89abcdef"]; ok {
		t.Fatalf("true orphan still reserved: %v", snap)
	}
}

// TestPortReservationOwners_IdentityAliases: every identity form of a saved
// shelf — persisted metadata ID and drifted computed ID — is protected. The
// fixture mirrors internal/data's drift test through the exported
// Save/Load API only.
func TestPortReservationOwners_IdentityAliases(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.MkdirAll(filepath.Join(target, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	store := data.NewWorkspaceStore(filepath.Join(base, "store"))

	ws := data.NewWorkspace("feat", "feat", "main",
		filepath.Join(target, "repo"), filepath.Join(link, "feat"))
	ws.Created = time.Now()
	ws.Shelved, ws.Archived = true, true
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	savedID := ws.MetadataID()

	// Create the worktree the symlink resolves to, then reload: ComputedID
	// now hashes the resolved real path and diverges from the stored key.
	if err := os.MkdirAll(filepath.Join(link, "feat"), 0o755); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(savedID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ComputedID() == savedID {
		t.Skip("drift fixture did not produce a divergent computed ID on this platform")
	}
	storedID, ok := loaded.StoredID()
	if !ok || storedID != savedID {
		t.Fatalf("StoredID = %q,%v want %q", storedID, ok, savedID)
	}

	// Reserve BOTH identity forms — the reservation could have been minted
	// under either. Every canonical alias must protect them.
	app, resStore := appWithShelf(t, nil, loaded)
	for _, id := range workspacesvc.WorkspaceMetadataIDs(loaded) {
		mustReserveCleanup(t, resStore, string(id))
	}
	mustReserveCleanup(t, resStore, "deadbeef01234567")

	statusWs := &data.Workspace{Name: "ws", Repo: t.TempDir(), Root: t.TempDir(), Branch: "feat"}
	statusInterval(t, app, statusWs)
	if app.overlays.runOutputReleaseCount != 1 {
		t.Fatalf("release count = %d, want 1 (all shelf aliases excluded)",
			app.overlays.runOutputReleaseCount)
	}
	cmd := dialogResultReleasePortReservations(app, common.DialogResult{
		ID: DialogReleasePortReservations, Confirmed: true, Value: "1",
	}, dialogContext{portReleaseCount: 1})
	res, _ := cmd().(reservationReleaseResultMsg)
	if res.err != nil || res.count != 1 {
		t.Fatalf("release = %+v, want 1,nil", res)
	}
	snap, _ := resStore.Snapshot()
	for _, id := range workspacesvc.WorkspaceMetadataIDs(loaded) {
		if _, ok := snap[string(id)]; !ok {
			t.Fatalf("alias %q released: %v", id, snap)
		}
	}
}

// TestPortReservationOwners_GuardsPreserved: the port owner set keeps every
// existing guard — live, creating, mutating, tagged-session — while adding
// only shelves; shelves must stay outside the live traversal set.
func TestPortReservationOwners_GuardsPreserved(t *testing.T) {
	shelf := shelvedWorkspace("shelf-guard")
	shelfID := string(shelf.MetadataID())
	live := &data.Workspace{Name: "live", Repo: t.TempDir(), Root: t.TempDir(), Branch: "feat"}
	liveID := string(live.MetadataID())

	app, resStore := appWithShelf(t, []*data.Workspace{live}, shelf)
	app.lifecycle = newWorkspaceLifecycleState()
	app.lifecycle.markCreatingWorkspace("creating-id-1", "/tmp/creating-root")
	mutating := &data.Workspace{Name: "mut", Repo: t.TempDir(), Root: t.TempDir(), Branch: "m"}
	app.lifecycle.markMutatingWorkspaceIDs(mutating, true)

	// The shelf stays out of the live collector entirely.
	liveIDs := app.collectKnownWorkspaceIDs()
	if liveIDs[shelfID] {
		t.Fatal("live collector included a shelf-only identity")
	}
	if !liveIDs[liveID] || !liveIDs["creating-id-1"] {
		t.Fatalf("live collector lost guards: %v", liveIDs)
	}

	ownerIDs := app.collectPortReservationOwnerIDs()
	for _, want := range []string{liveID, shelfID, "creating-id-1"} {
		if !ownerIDs[want] {
			t.Fatalf("owner set missing %q", want)
		}
	}
	for _, id := range workspacesvc.WorkspaceMetadataIDs(mutating) {
		if !ownerIDs[string(id)] {
			t.Fatalf("owner set missing mutating alias %q", id)
		}
	}

	// Shelves never leak into the live traversal.
	for _, p := range app.projects {
		for _, w := range p.Workspaces {
			if w.Shelved {
				t.Fatal("shelf appeared inside Project.Workspaces")
			}
		}
	}

	// Duplicate IDs across a second project collapse to one set entry;
	// an empty project list still yields the guards.
	emptyApp, _, _ := reclaimableStatusApp(t, nil)
	emptyApp.lifecycle = newWorkspaceLifecycleState()
	emptyApp.lifecycle.markCreatingWorkspace("creating-only", "/tmp/r")
	only := emptyApp.collectPortReservationOwnerIDs()
	if !only["creating-only"] || len(only) != 1 {
		t.Fatalf("empty-project owner set = %v", only)
	}

	// Reserve a mutating-alias + creating ID: probe must protect both.
	for _, id := range workspacesvc.WorkspaceMetadataIDs(mutating) {
		mustReserveCleanup(t, resStore, string(id))
	}
	mustReserveCleanup(t, resStore, "creating-id-1")
	mustReserveCleanup(t, resStore, liveID)
	mustReserveCleanup(t, resStore, "deadbeef01234567")
	statusWs := &data.Workspace{Name: "ws", Repo: t.TempDir(), Root: t.TempDir(), Branch: "feat"}
	statusInterval(t, app, statusWs)
	if app.overlays.runOutputReleaseCount != 1 {
		t.Fatalf("release count = %d, want 1", app.overlays.runOutputReleaseCount)
	}
}

// TestPortReservationOwners_LiveSessionStillProtected: a reservation whose
// owner is gone from every identity set but still owns an amux session is
// not reclaimable — the session probe stays authoritative.
func TestPortReservationOwners_LiveSessionStillProtected(t *testing.T) {
	shelf := shelvedWorkspace("shelf-session")
	app, resStore, ops := reclaimableStatusApp(t, nil)
	for i := range app.projects {
		app.projects[i].ShelvedWorkspaces = append(app.projects[i].ShelvedWorkspaces, *shelf)
	}
	if len(app.projects) == 0 {
		app.projects = append(app.projects, data.Project{ShelvedWorkspaces: []data.Workspace{*shelf}})
	}
	const sessionOwner = "deadbeef5ession"
	mustReserveCleanup(t, resStore, sessionOwner)
	mustReserveCleanup(t, resStore, "deadbeef01234567")
	ops.SessionsWithTagsFunc = func(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
		return []tmux.SessionTagValues{sessionTagRow(sessionOwner)}, nil
	}
	ws := &data.Workspace{Name: "ws", Repo: t.TempDir(), Root: t.TempDir(), Branch: "feat"}
	statusInterval(t, app, ws)
	if app.overlays.runOutputReleaseCount != 1 {
		t.Fatalf("release count = %d, want 1 (session owner excluded)",
			app.overlays.runOutputReleaseCount)
	}
}
