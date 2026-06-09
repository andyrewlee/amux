package dashboard

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func workspaceRowIndices(m *Model) []int {
	var idx []int
	for i := range m.rows {
		if m.rows[i].Type == RowWorkspace && m.rows[i].Workspace != nil {
			idx = append(idx, i)
		}
	}
	return idx
}

func projectWithoutWorkspace(wsList []data.Workspace, removeID string) data.Project {
	kept := make([]data.Workspace, 0, len(wsList))
	for i := range wsList {
		if string(wsList[i].ID()) == removeID {
			continue
		}
		kept = append(kept, wsList[i])
	}
	return data.Project{Name: "repo", Path: "/repo", Workspaces: kept}
}

// TestSetProjects_AnchorsCursorToPredecessorOnDelete proves deleting the selected
// (middle) workspace moves the cursor to the row ABOVE it, not the successor that
// slid up into the same index.
func TestSetProjects_AnchorsCursorToPredecessorOnDelete(t *testing.T) {
	wsList := []data.Workspace{
		*data.NewWorkspace("ws1", "feature1", "main", "/repo", "/repo/ws1"),
		*data.NewWorkspace("ws2", "feature2", "main", "/repo", "/repo/ws2"),
		*data.NewWorkspace("ws3", "feature3", "main", "/repo", "/repo/ws3"),
	}

	m := New()
	m.SetProjects([]data.Project{{Name: "repo", Path: "/repo", Workspaces: wsList}})

	rows := workspaceRowIndices(m)
	if len(rows) != 3 {
		t.Fatalf("expected 3 workspace rows, got %d", len(rows))
	}
	middle := rows[1]
	selectedID := string(m.rows[middle].Workspace.ID())
	predecessorID := string(m.rows[middle-1].Workspace.ID())
	successorID := string(m.rows[middle+1].Workspace.ID())
	m.cursor = middle

	m.SetProjects([]data.Project{projectWithoutWorkspace(wsList, selectedID)})

	if m.rows[m.cursor].Type != RowWorkspace || m.rows[m.cursor].Workspace == nil {
		t.Fatalf("expected cursor on a workspace row after delete, got %+v", m.rows[m.cursor])
	}
	landedID := string(m.rows[m.cursor].Workspace.ID())
	if landedID == successorID {
		t.Fatalf("cursor slid onto the successor %q instead of the predecessor", successorID)
	}
	if landedID != predecessorID {
		t.Fatalf("expected cursor anchored to predecessor %q, got %q", predecessorID, landedID)
	}
}

// TestSetProjects_PreservesSelectedWorkspaceWhenOtherDeleted proves removing a
// non-selected workspace keeps selection on the same workspace by identity.
func TestSetProjects_PreservesSelectedWorkspaceWhenOtherDeleted(t *testing.T) {
	wsList := []data.Workspace{
		*data.NewWorkspace("ws1", "feature1", "main", "/repo", "/repo/ws1"),
		*data.NewWorkspace("ws2", "feature2", "main", "/repo", "/repo/ws2"),
		*data.NewWorkspace("ws3", "feature3", "main", "/repo", "/repo/ws3"),
	}

	m := New()
	m.SetProjects([]data.Project{{Name: "repo", Path: "/repo", Workspaces: wsList}})

	rows := workspaceRowIndices(m)
	if len(rows) != 3 {
		t.Fatalf("expected 3 workspace rows, got %d", len(rows))
	}
	// Select the last workspace, then delete the first (a different one).
	selectedIdx := rows[2]
	selectedID := string(m.rows[selectedIdx].Workspace.ID())
	otherID := string(m.rows[rows[0]].Workspace.ID())
	m.cursor = selectedIdx

	m.SetProjects([]data.Project{projectWithoutWorkspace(wsList, otherID)})

	if m.rows[m.cursor].Type != RowWorkspace || m.rows[m.cursor].Workspace == nil {
		t.Fatalf("expected cursor on a workspace row, got %+v", m.rows[m.cursor])
	}
	if got := string(m.rows[m.cursor].Workspace.ID()); got != selectedID {
		t.Fatalf("expected selection preserved on %q after deleting a different workspace, got %q", selectedID, got)
	}
}
