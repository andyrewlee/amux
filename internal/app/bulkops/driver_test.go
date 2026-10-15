package bulkops

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// fakeSeams records the driver's interactions: which targets ran through
// which op (invocation order is drain order), the marks cleared at
// drain/abandon, and the summary toasts. reject lists names the "lifecycle
// guard" refuses — the op returns no cmds for them, mirroring a real
// rejection where no completion will ever arrive.
type fakeSeams struct {
	calls     []string
	reject    map[string]bool
	cleared   []string
	warns     []string
	successes []string
}

func noopCmd() tea.Msg { return nil }

func (f *fakeSeams) op(name string) func(*data.Project, *data.Workspace) []tea.Cmd {
	return func(_ *data.Project, ws *data.Workspace) []tea.Cmd {
		f.calls = append(f.calls, name+":"+ws.Name)
		if f.reject[ws.Name] {
			return nil
		}
		return []tea.Cmd{noopCmd}
	}
}

func newFakeDriver() (*Driver, *fakeSeams) {
	f := &fakeSeams{reject: map[string]bool{}}
	d := New(Seams{
		Shelve:     f.op("shelve"),
		Restore:    f.op("restore"),
		Delete:     f.op("delete"),
		ClearMarks: func(ids []string) { f.cleared = append(f.cleared, ids...) },
		Warn: func(m string) tea.Cmd {
			f.warns = append(f.warns, m)
			return noopCmd
		},
		Success: func(m string) tea.Cmd {
			f.successes = append(f.successes, m)
			return noopCmd
		},
	})
	return &d, f
}

func makeTarget(name string) Target {
	ws := &data.Workspace{
		Name:   name,
		Branch: name,
		Repo:   "/repo",
		Root:   "/repo/.amux/workspaces/" + name,
	}
	return Target{
		Project:   &data.Project{Name: "repo", Path: "/repo"},
		Workspace: ws,
		MarkID:    string(ws.MetadataID()),
	}
}

func TestShelveStartsFirstOpOnly(t *testing.T) {
	d, f := newFakeDriver()
	targets := []Target{makeTarget("a"), makeTarget("b"), makeTarget("c")}

	cmd := d.Start(Shelve, targets)
	if cmd == nil {
		t.Fatal("expected the first op's cmds")
	}
	if d.headID != targets[0].MarkID {
		t.Fatalf("head = %q, want %q", d.headID, targets[0].MarkID)
	}
	if len(d.queue) != 2 {
		t.Fatalf("queue = %d, want 2", len(d.queue))
	}
	// Only the head ran its per-row op — the drain is sequential.
	if len(f.calls) != 1 || f.calls[0] != "shelve:a" {
		t.Fatalf("ops = %v, want [shelve:a] only", f.calls)
	}
}

func TestShelveAdvancesSequentiallyAndDrains(t *testing.T) {
	d, f := newFakeDriver()
	targets := []Target{makeTarget("a"), makeTarget("b")}

	d.Start(Shelve, targets)

	// ws a completes → b becomes head.
	if cmd := d.Finished(targets[0].Workspace, true); cmd == nil {
		t.Fatal("expected next op's cmds after first completion")
	}
	if d.headID != targets[1].MarkID {
		t.Fatalf("head after first completion = %q, want %q", d.headID, targets[1].MarkID)
	}
	if d.succeeded != 1 {
		t.Fatalf("succeeded = %d, want 1", d.succeeded)
	}
	if len(f.calls) != 2 || f.calls[1] != "shelve:b" {
		t.Fatalf("ops = %v, want shelve:b second", f.calls)
	}

	// ws b completes → queue empty → summary toast + marks cleared + state
	// reset.
	if cmd := d.Finished(targets[1].Workspace, true); cmd == nil {
		t.Fatal("expected summary toast cmd at drain")
	}
	if d.Active() {
		t.Fatal("batch state not reset after drain")
	}
	if len(f.cleared) != 2 || f.cleared[0] != targets[0].MarkID || f.cleared[1] != targets[1].MarkID {
		t.Fatalf("marks cleared = %v, want both targets", f.cleared)
	}
	if len(f.successes) != 1 || f.successes[0] != "Shelved 2 of 2" {
		t.Fatalf("summary = %v, want 'Shelved 2 of 2'", f.successes)
	}
}

func TestShelveGuardRejectionSkipsAndCounts(t *testing.T) {
	d, f := newFakeDriver()
	targets := []Target{makeTarget("a"), makeTarget("b"), makeTarget("c")}
	// b is already in another lifecycle phase — the per-row guard rejects
	// it, so the drain must count it failed and move on.
	f.reject["b"] = true

	d.Start(Shelve, targets)
	// Finish a → advance tries b (rejected → failed++, skipped) → c is head.
	d.Finished(targets[0].Workspace, true)
	if d.failed != 1 {
		t.Fatalf("failed = %d, want 1 (b rejected)", d.failed)
	}
	if d.headID != targets[2].MarkID {
		t.Fatalf("head after rejected item = %q, want %q (c)", d.headID, targets[2].MarkID)
	}
	if len(f.calls) != 3 || f.calls[2] != "shelve:c" {
		t.Fatalf("ops = %v, want shelve:c after the rejection", f.calls)
	}

	d.Finished(targets[2].Workspace, true)
	if len(f.warns) != 1 || !strings.Contains(f.warns[0], "Shelved 2 of 3") || !strings.Contains(f.warns[0], "1 failed") {
		t.Fatalf("summary = %v, want 'Shelved 2 of 3 — 1 failed'", f.warns)
	}
}

func TestForeignCompletionIgnored(t *testing.T) {
	d, _ := newFakeDriver()
	targets := []Target{makeTarget("a"), makeTarget("b")}
	d.Start(Shelve, targets)

	// A manual shelve completing for a workspace that is not the queue
	// head must not advance or count toward the batch.
	foreign := makeTarget("manual")
	if cmd := d.Finished(foreign.Workspace, true); cmd != nil {
		t.Fatal("foreign completion produced a cmd — it must be ignored")
	}
	if d.headID != targets[0].MarkID {
		t.Fatalf("foreign completion moved head to %q", d.headID)
	}
	if d.succeeded != 0 || d.failed != 0 {
		t.Fatalf("foreign completion corrupted counters: %d/%d", d.succeeded, d.failed)
	}
}

// TestStartRejectsReentry proves a second confirmed batch while one drains
// is rejected — overwriting the state would orphan the in-flight head's
// completion and run two worktree ops in parallel.
func TestStartRejectsReentry(t *testing.T) {
	d, _ := newFakeDriver()
	first := []Target{makeTarget("a"), makeTarget("b")}
	if cmd := d.Start(Shelve, first); cmd == nil {
		t.Fatal("first batch must start")
	}
	headID, total := d.headID, d.total
	queueLen := len(d.queue)

	cmd := d.Start(Shelve, []Target{makeTarget("x"), makeTarget("y")})
	if cmd == nil {
		t.Fatal("rejection should still return a toast cmd")
	}
	if d.total != total || d.headID != headID || len(d.queue) != queueLen {
		t.Fatalf("re-entry overwrote the draining batch: %+v", d)
	}
	// A stale foreign completion still cannot corrupt the draining batch.
	if cmd := d.Finished(makeTarget("x").Workspace, true); cmd != nil {
		t.Fatal("foreign completion must not advance the batch")
	}
	if d.headID != headID {
		t.Fatal("foreign completion cleared the in-flight head")
	}
}

func TestTargetsFromItemsDropsNil(t *testing.T) {
	ok := makeTarget("y")
	targets := TargetsFromItems([]messages.BulkWorkspaceItem{
		{Project: nil, Workspace: &data.Workspace{Name: "x"}},
		{Project: ok.Project, Workspace: ok.Workspace},
	})
	if len(targets) != 1 || targets[0].Workspace.Name != "y" {
		t.Fatalf("nil-project item must be dropped: %+v", targets)
	}
}

// TestRestoreDrainsSequentially runs the restore kind through the same
// drain: per-row calls go through the restore seam and the summary uses
// the restore verb.
func TestRestoreDrainsSequentially(t *testing.T) {
	d, f := newFakeDriver()
	targets := []Target{makeTarget("a"), makeTarget("b")}

	if cmd := d.Start(Restore, targets); cmd == nil {
		t.Fatal("expected the first op's cmds")
	}
	if d.kind != Restore {
		t.Fatalf("kind = %v, want restore", d.kind)
	}
	if len(f.calls) != 1 || f.calls[0] != "restore:a" {
		t.Fatalf("ops = %v, want [restore:a] only", f.calls)
	}

	if cmd := d.Finished(targets[0].Workspace, true); cmd == nil {
		t.Fatal("expected next op's cmds")
	}
	if cmd := d.Finished(targets[1].Workspace, true); cmd == nil {
		t.Fatal("expected summary toast at drain")
	}
	if d.Active() {
		t.Fatal("batch not reset after drain")
	}
	if len(f.successes) != 1 || f.successes[0] != "Restored 2 of 2" {
		t.Fatalf("summary = %v, want 'Restored 2 of 2'", f.successes)
	}
}

// TestPurgeDrainsThroughDeleteOp proves the purge kind drives the delete
// seam and the summary uses the purge verb.
func TestPurgeDrainsThroughDeleteOp(t *testing.T) {
	d, f := newFakeDriver()
	targets := []Target{makeTarget("a"), makeTarget("b")}

	if cmd := d.Start(Purge, targets); cmd == nil {
		t.Fatal("expected the first op's cmds")
	}
	if len(f.calls) != 1 || f.calls[0] != "delete:a" {
		t.Fatalf("ops = %v, want delete:a (purge drains through delete)", f.calls)
	}
	d.Finished(targets[0].Workspace, true)
	d.Finished(targets[1].Workspace, true)
	if len(f.successes) != 1 || f.successes[0] != "Purged 2 of 2" {
		t.Fatalf("summary = %v, want 'Purged 2 of 2'", f.successes)
	}
	if len(f.cleared) != 2 {
		t.Fatalf("marks cleared = %v, want both targets", f.cleared)
	}
}

// TestPurgeGuardRejectionCounts proves a shelved row in another lifecycle
// phase is skipped+counted, not stalled, mid-purge.
func TestPurgeGuardRejectionCounts(t *testing.T) {
	d, f := newFakeDriver()
	targets := []Target{makeTarget("a"), makeTarget("b")}
	f.reject["a"] = true

	d.Start(Purge, targets)
	// a rejected → skipped, b becomes head.
	if d.failed != 1 || d.headID != targets[1].MarkID {
		t.Fatalf("rejected head should count+skip: %+v", d)
	}
}

// TestDeleteDrainsThroughDeleteOp proves marked live rows flow one at a
// time through the same per-row delete op a single D uses — the batch is
// sequencing, not a parallel op.
func TestDeleteDrainsThroughDeleteOp(t *testing.T) {
	d, f := newFakeDriver()
	targets := []Target{makeTarget("a"), makeTarget("b")}

	if cmd := d.Start(Delete, targets); cmd == nil {
		t.Fatal("expected the first op's cmds")
	}
	if len(f.calls) != 1 || f.calls[0] != "delete:a" {
		t.Fatalf("ops = %v, want [delete:a] only", f.calls)
	}
	d.Finished(targets[0].Workspace, true)
	d.Finished(targets[1].Workspace, true)
	if len(f.successes) != 1 || f.successes[0] != "Deleted 2 of 2" {
		t.Fatalf("summary = %v, want 'Deleted 2 of 2'", f.successes)
	}
}

// TestDeleteGuardRejectionCounts proves a live row that entered another
// lifecycle phase between dialog open and drain is skipped+counted, not
// stalled.
func TestDeleteGuardRejectionCounts(t *testing.T) {
	d, f := newFakeDriver()
	targets := []Target{makeTarget("a"), makeTarget("b")}
	f.reject["a"] = true

	d.Start(Delete, targets)
	if d.failed != 1 || d.headID != targets[1].MarkID {
		t.Fatalf("rejected head should count+skip: %+v", d)
	}
}

// TestAbandon covers the panic-recovery path: a wedge (head armed, queue
// non-empty) is dropped wholesale — its dashboard marks come down here
// since the drain will never run, the in-flight head's late completion
// no-ops on the cleared headID, and a fresh batch can start.
func TestAbandon(t *testing.T) {
	d, f := newFakeDriver()
	targets := []Target{makeTarget("a"), makeTarget("b")}
	d.Start(Shelve, targets)

	d.Abandon()

	if d.Active() {
		t.Fatal("batch survived Abandon")
	}
	if len(f.cleared) != 2 {
		t.Fatalf("abandoned batch did not drop its marks: %v", f.cleared)
	}
	// The in-flight head op's late completion must not corrupt anything:
	// headID was cleared with the batch, so Finished ignores it.
	if cmd := d.Finished(targets[0].Workspace, true); cmd != nil {
		t.Fatal("orphaned head completion produced a cmd against the cleared batch")
	}
	// And a fresh batch can start — the wedge is actually gone.
	if cmd := d.Start(Shelve, targets); cmd == nil {
		t.Fatal("post-abandon batch rejected — the wedge is still armed")
	}
}

func TestAbandonNoopWhenIdle(t *testing.T) {
	d, f := newFakeDriver()
	d.Abandon()
	if len(f.cleared) != 0 {
		t.Fatal("idle Abandon cleared marks")
	}
}
