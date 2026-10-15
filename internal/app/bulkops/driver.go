// Package bulkops owns the sequential bulk-operation driver — a marked
// set of workspaces drained one at a time through the kind's literal
// per-row lifecycle op, with completion-matched advancement and a batch
// summary at drain. The App supplies its whole surface through Seams, so
// the driver is unit-testable without an App fixture.
package bulkops

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// Kind identifies which per-row lifecycle op a batch drains through.
//
// NOTE: adding a new Kind must update the "Bulk Operations Flow" section
// in internal/app/MESSAGE_FLOW.md — it is the documented contract for
// this driver.
type Kind int

const (
	Shelve Kind = iota
	Restore
	Purge
	Delete
)

// Verb is the summary word for the finished batch ("Shelved 3 of 4").
func (k Kind) Verb() string {
	switch k {
	case Restore:
		return "Restored"
	case Purge:
		return "Purged"
	case Delete:
		return "Deleted"
	default:
		return "Shelved"
	}
}

// Target is one confirmed bulk-op item — the project travels with the
// workspace because dashboard marks can span projects.
type Target struct {
	Project   *data.Project
	Workspace *data.Workspace
	MarkID    string // MetadataID — used to clear the dashboard mark at drain
}

// TargetsFromItems converts the message's item list to driver targets,
// dropping entries that can't run the per-row op anyway.
func TargetsFromItems(items []messages.BulkWorkspaceItem) []Target {
	targets := make([]Target, 0, len(items))
	for _, it := range items {
		if it.Project == nil || it.Workspace == nil {
			continue
		}
		targets = append(targets, Target{
			Project:   it.Project,
			Workspace: it.Workspace,
			MarkID:    string(it.Workspace.MetadataID()),
		})
	}
	return targets
}

// Seams are the driver's whole App surface, injected at construction.
// Shelve/Restore/Delete run the same guarded per-row handlers a single
// op runs; ClearMarks drops the batch's dashboard marks; Warn/Success
// emit the summary toast.
type Seams struct {
	Shelve  func(p *data.Project, ws *data.Workspace) []tea.Cmd
	Restore func(p *data.Project, ws *data.Workspace) []tea.Cmd
	// Delete serves both Purge and Delete — live and shelved rows share
	// the delete flow; the per-row handler tolerates the absent worktree
	// on purge.
	Delete     func(p *data.Project, ws *data.Workspace) []tea.Cmd
	ClearMarks func(markIDs []string)
	Warn       func(msg string) tea.Cmd
	Success    func(msg string) tea.Cmd
}

// Driver is the sequential bulk-op state machine shared by shelve,
// restore, purge, and delete. The queue drains one workspace at a time
// through the kind's per-row op — the same guarded, spinner-backed op a
// single workspace runs — so worktree add/removals never run in
// parallel. headID is the in-flight op's MetadataID: the matching
// completion message advances the queue, while a foreign completion (a
// manual op interleaving) is ignored and cannot corrupt the accounting.
//
// All methods run on the Update goroutine — the driver carries no
// synchronization of its own.
type Driver struct {
	seams Seams

	kind      Kind
	queue     []Target
	markIDs   []string // every target's mark — cleared on the dashboard at drain
	headID    string
	total     int
	succeeded int
	failed    int
}

// New returns a zero-state driver bound to the app's seams.
func New(seams Seams) Driver {
	return Driver{seams: seams}
}

// Active reports whether any batch is in progress.
func (d *Driver) Active() bool { return d.total > 0 }

// ActiveFor reports whether a batch of the given kind is draining — used
// to suppress that op's per-row toasts in favor of the batch summary at
// drain without eating toasts from an unrelated kind.
func (d *Driver) ActiveFor(kind Kind) bool {
	return d.Active() && d.kind == kind
}

// HeadID exposes the in-flight op's MetadataID for observability.
func (d *Driver) HeadID() string { return d.headID }

// Counters expose the batch's accounting for observability.
func (d *Driver) Counters() (total, succeeded, failed int) {
	return d.total, d.succeeded, d.failed
}

// Start initializes the batch and kicks the first op. A second batch
// while one drains is rejected, not queued — overwriting the state would
// orphan the in-flight op's headID and run two drains in parallel.
func (d *Driver) Start(kind Kind, targets []Target) tea.Cmd {
	if d.Active() {
		logging.Warn("Bulk op rejected: a batch is already draining")
		return d.seams.Warn("A bulk operation is already running")
	}
	markIDs := make([]string, 0, len(targets))
	for _, t := range targets {
		markIDs = append(markIDs, t.MarkID)
	}
	d.kind = kind
	d.queue = targets
	d.markIDs = markIDs
	d.headID = ""
	d.total = len(targets)
	d.succeeded = 0
	d.failed = 0
	return d.advance()
}

// Finished records the in-flight op's completion and advances. The ws
// must match the queue head — a completion for any other workspace (a
// manual op interleaving with the batch) belongs to that flow and is
// left to its own accounting.
func (d *Driver) Finished(ws *data.Workspace, ok bool) tea.Cmd {
	if d.headID == "" || ws == nil || string(ws.MetadataID()) != d.headID {
		return nil
	}
	d.headID = ""
	if ok {
		d.succeeded++
	} else {
		d.failed++
	}
	return d.advance()
}

// Abandon drops the batch mid-drain — the panic-recovery path. The
// in-flight head op still completes through its own message (Finished
// no-ops on the cleared headID, so a foreign completion cannot corrupt a
// future batch), and its marks come down here since the drain will never
// run.
func (d *Driver) Abandon() {
	if !d.Active() {
		return
	}
	d.seams.ClearMarks(d.markIDs)
	d.reset()
}

// advance pops the next target and drives it through the kind's per-row
// op. A guard rejection returns no cmds — that target is counted failed
// and skipped immediately rather than stalling the drain. An empty queue
// means the batch is done: summarize and clear marks.
func (d *Driver) advance() tea.Cmd {
	for len(d.queue) > 0 {
		next := d.queue[0]
		d.queue = d.queue[1:]
		d.headID = string(next.Workspace.MetadataID())
		var cmds []tea.Cmd
		switch d.kind {
		case Restore:
			cmds = d.seams.Restore(next.Project, next.Workspace)
		case Purge, Delete:
			cmds = d.seams.Delete(next.Project, next.Workspace)
		default: // Shelve
			cmds = d.seams.Shelve(next.Project, next.Workspace)
		}
		if len(cmds) == 0 {
			// Rejected by the lifecycle guard (already in another phase)
			// — count and move on; no completion will arrive for it.
			d.headID = ""
			d.failed++
			continue
		}
		return common.SafeBatch(cmds...)
	}
	return d.finish()
}

// finish emits the batch summary and clears the batch members' dashboard
// marks — the rows they pointed at are shelved, restored, or purged (or
// failed) now.
func (d *Driver) finish() tea.Cmd {
	if d.total == 0 {
		return nil
	}
	markIDs := d.markIDs
	total, succeeded, failed := d.total, d.succeeded, d.failed
	verb := d.kind.Verb()
	d.reset()
	d.seams.ClearMarks(markIDs)
	if failed > 0 {
		return d.seams.Warn(fmt.Sprintf("%s %d of %d — %d failed", verb, succeeded, total, failed))
	}
	return d.seams.Success(fmt.Sprintf("%s %d of %d", verb, succeeded, total))
}

// reset clears the batch state, keeping the injected seams.
func (d *Driver) reset() {
	d.kind = Shelve
	d.queue = nil
	d.markIDs = nil
	d.headID = ""
	d.total = 0
	d.succeeded = 0
	d.failed = 0
}
