package workspacesvc

// Retry, interruption, and failure-shape tests for Service.RenameProject —
// split from workspace_service_rename_test.go under the 500-line cap. The
// fixture and helpers live there; these tests exercise what happens when a
// rename is interrupted, skipped, refused, or re-issued.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestRenameProjectRetryCompletesInterruptedMigration(t *testing.T) {
	f := newRenameFixture(t)
	wsA := f.addWorktree(t, "feature-a")
	wsB := f.addWorktree(t, "feature-b")
	f.discoverFunc(wsA, wsB)

	// Simulate the interrupted state for feature-a: the directory already
	// moved (a prior run's git move landed, so git's registration — what
	// discovery reports — points at the new location) but the record still
	// points at the old container.
	if err := os.MkdirAll(filepath.Join(f.root, "checkfu-old"), 0o755); err != nil {
		t.Fatalf("mkdir container: %v", err)
	}
	movedA := filepath.Join(f.root, "checkfu-old", "feature-a")
	if err := os.Rename(wsA.Root, movedA); err != nil {
		t.Fatalf("pre-move feature-a: %v", err)
	}
	// Discovery reports the new root (git's registration moved with the
	// worktree); the record still carries the old one.
	f.discoverFunc(
		func() *data.Workspace {
			ws := *wsA
			ws.Root = movedA
			return &ws
		}(),
		wsB,
	)

	msg := runRenameCmd(t, f.svc.RenameProject(f.project, "checkfu-old"))
	renamed := asProjectRenamed(t, msg)
	if renamed.Moved != 2 || renamed.Failed != 0 || renamed.Err != nil {
		t.Fatalf("retry must finish both worktrees, got %+v", renamed)
	}
	roots := f.savedRoots()
	if !roots[movedA] || !roots[filepath.Join(f.root, "checkfu-old", "feature-b")] {
		t.Fatalf("both records must be rebound after the retry: %v", roots)
	}
}

func TestRenameProjectRetryCompletesWhenDiscoveryStillReportsOldRoot(t *testing.T) {
	f := newRenameFixture(t)
	wsA := f.addWorktree(t, "feature-a")
	wsB := f.addWorktree(t, "feature-b")
	f.discoverFunc(wsA, wsB)

	// The same interrupted state seen through a discovery pass that still
	// reports the old root (e.g. a stale snapshot): the directory is gone
	// from the old container, the destination exists, the record lags.
	if err := os.MkdirAll(filepath.Join(f.root, "checkfu-old"), 0o755); err != nil {
		t.Fatalf("mkdir container: %v", err)
	}
	if err := os.Rename(wsA.Root, filepath.Join(f.root, "checkfu-old", "feature-a")); err != nil {
		t.Fatalf("pre-move feature-a: %v", err)
	}

	msg := runRenameCmd(t, f.svc.RenameProject(f.project, "checkfu-old"))
	renamed := asProjectRenamed(t, msg)
	if renamed.Moved != 2 || renamed.Failed != 0 || renamed.Err != nil {
		t.Fatalf("retry must finish both worktrees, got %+v", renamed)
	}
	roots := f.savedRoots()
	if !roots[filepath.Join(f.root, "checkfu-old", "feature-a")] ||
		!roots[filepath.Join(f.root, "checkfu-old", "feature-b")] {
		t.Fatalf("both records must be rebound after the retry: %v", roots)
	}
}

func TestRenameProjectToCurrentNameIsANoop(t *testing.T) {
	f := newRenameFixture(t)
	wsA := f.addWorktree(t, "feature-a")
	f.discoverFunc(wsA)

	msg := runRenameCmd(t, f.svc.RenameProject(f.project, f.project.Name))
	renamed := asProjectRenamed(t, msg)
	if renamed.Err != nil || renamed.Moved != 0 || renamed.Failed != 0 || renamed.Skipped != 0 {
		t.Fatalf("rename to the current name must be a no-op, got %+v", renamed)
	}
	if len(*f.moves) != 0 {
		t.Fatalf("no worktree may move when the name does not change")
	}
}

func TestRenameProjectSameNameFinishesLeftoverWorktrees(t *testing.T) {
	f := newRenameFixture(t)
	// Retry shape after a failed first run: the registry already holds
	// checkfu-old but a worktree never moved. Same name must reconcile —
	// and must not rewrite the registry entry it already matches.
	wsA := f.addWorktree(t, "feature-a")
	f.discoverFunc(wsA)
	project := data.NewProjectNamed("checkfu-old", f.repo)

	msg := runRenameCmd(t, f.svc.RenameProject(project, "checkfu-old"))
	renamed := asProjectRenamed(t, msg)
	if renamed.Err != nil || renamed.Failed != 0 || renamed.Moved != 1 {
		t.Fatalf("same-name retry must move the leftover worktree: %+v", renamed)
	}
	if calls := f.registry.RenamedCalls(); len(calls) != 0 {
		t.Fatalf("registry write must be skipped on the same-name retry: %v", calls)
	}
	if _, err := os.Stat(filepath.Join(f.root, "checkfu-old", "feature-a")); err != nil {
		t.Fatalf("leftover worktree must reach the new container: %v", err)
	}
	roots := f.savedRoots()
	if !roots[filepath.Join(f.root, "checkfu-old", "feature-a")] {
		t.Fatalf("leftover worktree's record must follow: %v", roots)
	}
}

func TestRenameProjectMovesWorktreeWithoutRecord(t *testing.T) {
	f := newRenameFixture(t)
	// A worktree in the container that no tool ever recorded — the live
	// collision state has dozens (plain `git worktree add` from other
	// tooling). It must move with its container, no rebind, counted moved.
	bareRoot := filepath.Join(f.root, f.project.Name, "unrecorded")
	if err := os.MkdirAll(bareRoot, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	ws := data.NewWorkspace("unrecorded", "unrecorded", "main", f.repo, bareRoot)
	f.discoverFunc(ws)

	msg := runRenameCmd(t, f.svc.RenameProject(f.project, "checkfu-old"))
	renamed := asProjectRenamed(t, msg)
	if renamed.Err != nil || renamed.Failed != 0 || renamed.Moved != 1 {
		t.Fatalf("recordless worktree must move cleanly: %+v", renamed)
	}
	if _, err := os.Stat(filepath.Join(f.root, "checkfu-old", "unrecorded")); err != nil {
		t.Fatalf("recordless worktree must reach the new container: %v", err)
	}
}

func TestRenameProjectContainerCreationFailureCarriesErr(t *testing.T) {
	f := newRenameFixture(t)
	wsA := f.addWorktree(t, "feature-a")
	f.discoverFunc(wsA)
	// A regular file where the new container must go: MkdirAll fails before
	// any worktree is touched — Failed stays 0 while Err is set. The TUI and
	// CLI key their failure surfacing on Err, not the count, precisely so
	// this shape cannot masquerade as success.
	blocker := filepath.Join(f.root, "blocked")
	if err := os.WriteFile(blocker, []byte("occupied"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	msg := runRenameCmd(t, f.svc.RenameProject(f.project, "blocked"))
	renamed := asProjectRenamed(t, msg)
	if renamed.Err == nil {
		t.Fatalf("container creation failure must carry Err: %+v", renamed)
	}
	if renamed.Failed != 0 || renamed.Moved != 0 {
		t.Fatalf("no per-worktree work happened: %+v", renamed)
	}
	if _, err := os.Stat(wsA.Root); err != nil {
		t.Fatalf("worktree must be untouched: %v", err)
	}
}

func TestRenameProjectCaseOnlyRefusedOnCaseInsensitiveFS(t *testing.T) {
	f := newRenameFixture(t)
	// A case-only rename (capitalization fix) where both spellings resolve
	// to the same physical container — the default on case-insensitive APFS
	// — must be refused up front: record and git identity comparisons are
	// case-sensitive strings, so the worktrees would end up split across
	// two spellings of one directory.
	cased := data.NewProjectNamed("CaseName", f.repo)
	wsRoot := filepath.Join(f.root, "CaseName", "feature-a")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	ws := data.NewWorkspace("feature-a", "feature-a", "main", f.repo, wsRoot)
	f.discoverFunc(ws)

	msg := runRenameCmd(t, f.svc.RenameProject(cased, "casename"))

	if same, ok := data.SamePhysicalDir(filepath.Join(f.root, "CaseName"), filepath.Join(f.root, "casename")); ok && same {
		errMsg, isErr := msg.(messages.Error)
		if !isErr {
			t.Fatalf("case-only rename on a case-insensitive filesystem must be refused, got %+v", msg)
		}
		if !strings.Contains(errMsg.Err.Error(), "letter case") {
			t.Fatalf("refusal must explain the case constraint: %v", errMsg.Err)
		}
		if _, err := os.Stat(wsRoot); err != nil {
			t.Fatalf("worktree must be untouched by the refusal: %v", err)
		}
		return
	}
	// Case-sensitive filesystem: the spellings are distinct directories and
	// the rename proceeds as a normal container migration.
	renamed := asProjectRenamed(t, msg)
	if renamed.Err != nil || renamed.Failed != 0 || renamed.Moved != 1 {
		t.Fatalf("case rename on a case-sensitive filesystem must migrate normally: %+v", renamed)
	}
	if _, err := os.Stat(filepath.Join(f.root, "casename", "feature-a")); err != nil {
		t.Fatalf("worktree must reach the new-case container: %v", err)
	}
}

func TestRenameProjectRetryMovesWorktreesFromRetiredContainer(t *testing.T) {
	f := newRenameFixture(t)
	// The stranding shape the retry must recover from: the project's stored
	// name was minted (basename collision), the first rename skipped its
	// only worktree (scripts running), and the retry's caller already
	// carries the NEW name — the retired old-name container is nobody's
	// alias then. The unclaimed-container source rule must still find it.
	minted := f.project.Name + "-2"
	wsRoot := filepath.Join(f.root, minted, "feature-a")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	ws := data.NewWorkspace("feature-a", "feature-a", "main", f.repo, wsRoot)
	if err := f.store.Save(ws); err != nil {
		t.Fatalf("save record: %v", err)
	}
	f.discoverFunc(ws)
	f.svc.scriptsRunning = func(*data.Workspace) bool { return true }
	project := data.NewProjectNamed(minted, f.repo)

	msg := runRenameCmd(t, f.svc.RenameProject(project, "gamma"))
	renamed := asProjectRenamed(t, msg)
	if renamed.Err != nil || renamed.Skipped != 1 {
		t.Fatalf("first rename must skip the busy worktree: %+v", renamed)
	}

	// Scripts closed; the registry already says gamma. Re-issue and the
	// stranded worktree must leave the retired minted container.
	f.svc.scriptsRunning = nil
	msg = runRenameCmd(t, f.svc.RenameProject(data.NewProjectNamed("gamma", f.repo), "gamma"))
	renamed = asProjectRenamed(t, msg)
	if renamed.Err != nil || renamed.Failed != 0 || renamed.Moved != 1 {
		t.Fatalf("retry must move the worktree stranded in the retired container: %+v", renamed)
	}
	if _, err := os.Stat(filepath.Join(f.root, "gamma", "feature-a")); err != nil {
		t.Fatalf("worktree must reach the new container on retry: %v", err)
	}
	roots := f.savedRoots()
	if !roots[filepath.Join(f.root, "gamma", "feature-a")] {
		t.Fatalf("record must follow the retried move: %v", roots)
	}
}

func TestRenameProjectLeavesOutofBandRemovedWorktreeRecordAlone(t *testing.T) {
	f := newRenameFixture(t)
	// Discovery still reports the worktree (git's registration is stale
	// after an out-of-band rm -rf), but the directory is gone and no
	// destination exists. Rebinding the record to the new container's path
	// would point it at a directory nothing will ever create; the record
	// must stay with the registration it still matches.
	wsA := f.addWorktree(t, "feature-a")
	if err := os.RemoveAll(wsA.Root); err != nil {
		t.Fatalf("remove worktree dir: %v", err)
	}
	f.discoverFunc(wsA)

	msg := runRenameCmd(t, f.svc.RenameProject(f.project, "checkfu-old"))
	renamed := asProjectRenamed(t, msg)
	if renamed.Err != nil || renamed.Failed != 0 || renamed.Moved != 0 {
		t.Fatalf("an out-of-band removal is neither a move nor a failure: %+v", renamed)
	}
	roots := f.savedRoots()
	if !roots[wsA.Root] {
		t.Fatalf("record must keep the root its stale registration matches: %v", roots)
	}
}

func TestRenameProjectSameNameRetryRebindsStrandedRecord(t *testing.T) {
	f := newRenameFixture(t)
	// Reloaded-model retry shape: the registry already says "beta" (what
	// the CLI builds its project from and the TUI reloads into), the
	// worktree reached beta's container in a prior run, but the record
	// still claims the retired "alpha" spelling — the residue of a crash
	// between git's move and the rebind. The same-name retry must still
	// find the record by leaf and rebind it.
	f.registry.EntriesFunc = func() ([]data.RegistryEntry, error) {
		return []data.RegistryEntry{{Name: "beta", Path: f.repo}}, nil
	}
	wsRoot := filepath.Join(f.root, "beta", "feature-a")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	ws := data.NewWorkspace("feature-a", "feature-a", "main", f.repo, wsRoot)
	f.discoverFunc(ws)
	stranded := data.NewWorkspace("feature-a", "feature-a", "main", f.repo, filepath.Join(f.root, "alpha", "feature-a"))
	if err := f.store.Save(stranded); err != nil {
		t.Fatalf("save stranded record: %v", err)
	}

	msg := runRenameCmd(t, f.svc.RenameProject(data.NewProjectNamed("beta", f.repo), "beta"))
	renamed := asProjectRenamed(t, msg)
	if renamed.Err != nil || renamed.Failed != 0 || renamed.Moved != 1 {
		t.Fatalf("same-name retry must rebind the stranded record: %+v", renamed)
	}
	if len(*f.moves) != 0 {
		t.Fatalf("record-only reconciliation moves nothing: %v", *f.moves)
	}
	roots := f.savedRoots()
	if !roots[wsRoot] {
		t.Fatalf("stranded record must claim the live root: %v", roots)
	}
}

func TestRenameProjectChainRenameCarriesStrandedRecord(t *testing.T) {
	f := newRenameFixture(t)
	// Two renames deep: the worktree sits in beta's container while its
	// record is still stranded in the retired alpha container. Renaming
	// beta→gamma must move the worktree and carry the stranded record to
	// gamma's container — not strand it a third time.
	f.registry.EntriesFunc = func() ([]data.RegistryEntry, error) {
		return []data.RegistryEntry{{Name: "beta", Path: f.repo}}, nil
	}
	wsRoot := filepath.Join(f.root, "beta", "feature-a")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	ws := data.NewWorkspace("feature-a", "feature-a", "main", f.repo, wsRoot)
	f.discoverFunc(ws)
	stranded := data.NewWorkspace("feature-a", "feature-a", "main", f.repo, filepath.Join(f.root, "alpha", "feature-a"))
	if err := f.store.Save(stranded); err != nil {
		t.Fatalf("save stranded record: %v", err)
	}

	msg := runRenameCmd(t, f.svc.RenameProject(data.NewProjectNamed("beta", f.repo), "gamma"))
	renamed := asProjectRenamed(t, msg)
	if renamed.Err != nil || renamed.Failed != 0 || renamed.Moved != 1 {
		t.Fatalf("chain rename must move the worktree and its record: %+v", renamed)
	}
	dest := filepath.Join(f.root, "gamma", "feature-a")
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("worktree must reach gamma's container: %v", err)
	}
	roots := f.savedRoots()
	if !roots[dest] {
		t.Fatalf("stranded record must follow to gamma's container: %v", roots)
	}
}

func TestRenameProjectRecordlessWorktreeDoesNotStealNeighborsRecord(t *testing.T) {
	f := newRenameFixture(t)
	// A recorded workspace "w" whose scripts are running (skipped by the
	// migration), and a recordless worktree with the same leaf under an
	// unclaimed container — both migration sources, but the record belongs
	// to the busy worktree. The recordless sibling must move without
	// inheriting the neighbor's identity.
	busy := f.addWorktree(t, "w")
	scratchRoot := filepath.Join(f.root, "scratch", "w")
	if err := os.MkdirAll(scratchRoot, 0o755); err != nil {
		t.Fatalf("mkdir recordless worktree: %v", err)
	}
	plain := data.NewWorkspace("w", "w", "main", f.repo, scratchRoot)
	f.discoverFunc(plain, busy)
	f.svc.scriptsRunning = func(ws *data.Workspace) bool {
		return data.SamePath(ws.Root, busy.Root)
	}

	msg := runRenameCmd(t, f.svc.RenameProject(f.project, "gamma"))
	renamed := asProjectRenamed(t, msg)
	if renamed.Err != nil || renamed.Failed != 0 || renamed.Skipped != 1 || renamed.Moved != 1 {
		t.Fatalf("recordless sibling moves, busy worktree skips: %+v", renamed)
	}
	if _, err := os.Stat(filepath.Join(f.root, "gamma", "w")); err != nil {
		t.Fatalf("the recordless sibling itself must move: %v", err)
	}
	roots := f.savedRoots()
	if !roots[busy.Root] {
		t.Fatalf("the busy workspace's record must stay with its worktree: %v", roots)
	}
	if roots[filepath.Join(f.root, "gamma", "w")] {
		t.Fatalf("the recordless sibling must not inherit the busy workspace's record: %v", roots)
	}
}

func TestRenameProjectContestedLeafDefersRecordReconciliation(t *testing.T) {
	f := newRenameFixture(t)
	// A recordless same-leaf sibling collides with the worktree already
	// sitting at the destination: the collision is reported loudly, and
	// with two same-named worktrees the code cannot tell whose record a
	// dead spelling is — the stranded record is left for a retry after the
	// user renames one of them, never silently attached to either.
	atDest := filepath.Join(f.root, "gamma", "w")
	if err := os.MkdirAll(atDest, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	ws := data.NewWorkspace("w", "w", "main", f.repo, atDest)
	strandedRoot := filepath.Join(f.root, "alpha", "w")
	stranded := data.NewWorkspace("w", "w", "main", f.repo, strandedRoot)
	if err := f.store.Save(stranded); err != nil {
		t.Fatalf("save stranded record: %v", err)
	}
	scratchRoot := filepath.Join(f.root, "scratch", "w")
	if err := os.MkdirAll(scratchRoot, 0o755); err != nil {
		t.Fatalf("mkdir sibling: %v", err)
	}
	sibling := data.NewWorkspace("w", "w", "main", f.repo, scratchRoot)
	f.discoverFunc(sibling, ws)

	msg := runRenameCmd(t, f.svc.RenameProject(f.project, "gamma"))
	renamed := asProjectRenamed(t, msg)
	if renamed.Err == nil || renamed.Failed != 1 {
		t.Fatalf("the sibling's destination collision must fail loudly: %+v", renamed)
	}
	if renamed.Moved != 0 {
		t.Fatalf("a contested leaf reconciles no record: %+v", renamed)
	}
	roots := f.savedRoots()
	if !roots[strandedRoot] {
		t.Fatalf("the stranded record must wait for disambiguation, not attach to either worktree: %v", roots)
	}
	if roots[atDest] {
		t.Fatalf("the record must not reach the contested destination: %v", roots)
	}
}

func TestRenameProjectRemovedWorktreeRecordDoesNotFollowSiblingToDestination(t *testing.T) {
	f := newRenameFixture(t)
	// Out-of-band removal plus a recordless same-leaf sibling in an
	// unclaimed container, sibling first in discovery: the sibling moves,
	// but the removed worktree's record must not follow it to the
	// destination — with two same-named worktrees, the directory that lands
	// there cannot be assumed to be the removed one's own.
	owner := f.addWorktree(t, "w")
	if err := os.RemoveAll(owner.Root); err != nil {
		t.Fatalf("remove owner worktree: %v", err)
	}
	scratchRoot := filepath.Join(f.root, "scratch", "w")
	if err := os.MkdirAll(scratchRoot, 0o755); err != nil {
		t.Fatalf("mkdir sibling: %v", err)
	}
	sibling := data.NewWorkspace("w", "w", "main", f.repo, scratchRoot)
	f.discoverFunc(sibling, owner) // sibling first — the hostile order

	msg := runRenameCmd(t, f.svc.RenameProject(f.project, "gamma"))
	renamed := asProjectRenamed(t, msg)
	if renamed.Err == nil || renamed.Failed != 1 {
		t.Fatalf("the removed worktree's contested destination must fail loudly: %+v", renamed)
	}
	if renamed.Moved != 1 {
		t.Fatalf("the recordless sibling still moves: %+v", renamed)
	}
	roots := f.savedRoots()
	if !roots[owner.Root] {
		t.Fatalf("the removed worktree's record must stay with its stale registration: %v", roots)
	}
}

func TestRenameProjectRecordlessWorktreeDoesNotTakeContestedStrandedRecord(t *testing.T) {
	f := newRenameFixture(t)
	// The normal branch's contested gate: a recordless worktree in the old
	// managed container and a same-leaf sibling whose record is stranded at
	// a dead spelling — the recordless one must move without inheriting the
	// stranded record, whichever order discovery reports. Recordless-first
	// is the hostile order.
	managedRoot := filepath.Join(f.root, f.project.Name, "w")
	if err := os.MkdirAll(managedRoot, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	plain := data.NewWorkspace("w", "w", "main", f.repo, managedRoot)
	strandedRoot := filepath.Join(f.root, "alpha", "w")
	stranded := data.NewWorkspace("w", "w", "main", f.repo, strandedRoot)
	if err := f.store.Save(stranded); err != nil {
		t.Fatalf("save stranded record: %v", err)
	}
	scratchRoot := filepath.Join(f.root, "scratch", "w")
	if err := os.MkdirAll(scratchRoot, 0o755); err != nil {
		t.Fatalf("mkdir sibling: %v", err)
	}
	sibling := data.NewWorkspace("w", "w", "main", f.repo, scratchRoot)
	f.discoverFunc(plain, sibling) // recordless first — the hostile order

	msg := runRenameCmd(t, f.svc.RenameProject(f.project, "gamma"))
	renamed := asProjectRenamed(t, msg)
	if renamed.Err == nil || renamed.Failed != 1 {
		t.Fatalf("the sibling's destination collision must fail loudly: %+v", renamed)
	}
	if renamed.Moved != 1 {
		t.Fatalf("the recordless worktree still moves: %+v", renamed)
	}
	roots := f.savedRoots()
	if !roots[strandedRoot] {
		t.Fatalf("the stranded record must stay at its dead spelling: %v", roots)
	}
	if roots[filepath.Join(f.root, "gamma", "w")] {
		t.Fatalf("no record may follow the recordless worktree to the contested destination: %v", roots)
	}
}
