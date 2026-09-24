package dashboard

import (
	"github.com/andyrewlee/amux/internal/messages"
)

// Workspace marks — the selection set bulk lifecycle ops apply to. Marks
// are keyed by string(ws.MetadataID()): the persisted store key survives
// row rebuilds, renames, and NormalizePath drift, so a mark outlives the
// row object it was set on. Marks are never pruned on rebuild — a marked
// workspace whose row vanished simply never matches when an op collects
// items from live rows.

// toggleMarkAtCursor flips the focused row's mark. Only rows that carry a
// workspace can be marked.
func (m *Model) toggleMarkAtCursor() {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return
	}
	row := m.rows[m.cursor]
	if (row.Type != RowWorkspace && row.Type != RowShelved) || row.Workspace == nil {
		return
	}
	id := string(row.Workspace.MetadataID())
	if m.marked[id] {
		delete(m.marked, id)
	} else {
		m.marked[id] = true
	}
}

// clearMarks drops every mark (esc). Marks also clear per workspace as a
// bulk op drains — see ClearMarks.
func (m *Model) clearMarks() {
	clear(m.marked)
}

// ClearMarks drops the named marks — called by the app when a bulk op
// drains so acted-on rows do not stay marked for the next op.
func (m *Model) ClearMarks(ids []string) {
	for _, id := range ids {
		delete(m.marked, id)
	}
	m.markContentDirty()
}

// MarkWorkspaceIDs sets marks directly by workspace MetadataID — the
// write side of ClearMarks, used by the app/tests rather than key input.
func (m *Model) MarkWorkspaceIDs(ids []string) {
	for _, id := range ids {
		m.marked[id] = true
	}
	m.markContentDirty()
}

// MarkedCount reports how many marks exist — surfaced in the help line.
func (m *Model) MarkedCount() int {
	return len(m.marked)
}

// isMarked reports whether the row's workspace is marked.
func (m *Model) isMarked(row Row) bool {
	if row.Workspace == nil {
		return false
	}
	return m.marked[string(row.Workspace.MetadataID())]
}

// markedShelveItems collects the marked rows the shelve op can act on —
// live workspace rows only, in display order. Shelved rows keep their
// marks for bulk restore/purge; their absence here is what stops shelve
// acting on a worktree that no longer exists.
func (m *Model) markedShelveItems() []messages.BulkWorkspaceItem {
	var items []messages.BulkWorkspaceItem
	for _, row := range m.rows {
		if row.Type == RowWorkspace && row.Workspace != nil && m.isMarked(row) {
			items = append(items, messages.BulkWorkspaceItem{
				Project:   row.Project,
				Workspace: row.Workspace,
			})
		}
	}
	return items
}

// markedShelvedItems collects the marked rows bulk restore/purge act on —
// shelved rows only, in display order. Restricting the collector to
// RowShelved is what keeps live workspaces out of a bulk purge: a mark on
// a live row simply never matches here.
func (m *Model) markedShelvedItems() []messages.BulkWorkspaceItem {
	var items []messages.BulkWorkspaceItem
	for _, row := range m.rows {
		if row.Type == RowShelved && row.Workspace != nil && m.isMarked(row) {
			items = append(items, messages.BulkWorkspaceItem{
				Project:   row.Project,
				Workspace: row.Workspace,
			})
		}
	}
	return items
}
