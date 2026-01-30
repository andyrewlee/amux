package sidebar

import (
	"testing"

	"github.com/andyrewlee/amux/internal/git"
)

func TestRebuildDisplayListSeparatesSections(t *testing.T) {
	m := New()
	m.gitStatus = &git.StatusResult{
		Staged: []git.Change{
			{Path: "staged.go", Kind: git.ChangeModified, Staged: true},
		},
		Unstaged: []git.Change{
			{Path: "modified.go", Kind: git.ChangeModified},
		},
		Untracked: []git.Change{
			{Path: "new.go", Kind: git.ChangeUntracked},
		},
	}

	m.rebuildDisplayList()

	// Expect: Staged header, staged file, Unstaged header, unstaged file, Untracked header, untracked file
	if len(m.displayItems) != 6 {
		t.Fatalf("expected 6 display items, got %d", len(m.displayItems))
	}

	// Staged header
	if !m.displayItems[0].isHeader || m.displayItems[0].header != "Staged (1)" {
		t.Errorf("expected Staged header, got %+v", m.displayItems[0])
	}
	if m.displayItems[1].change.Path != "staged.go" {
		t.Errorf("expected staged.go, got %s", m.displayItems[1].change.Path)
	}

	// Unstaged header
	if !m.displayItems[2].isHeader || m.displayItems[2].header != "Unstaged (1)" {
		t.Errorf("expected Unstaged header, got %+v", m.displayItems[2])
	}
	if m.displayItems[3].change.Path != "modified.go" {
		t.Errorf("expected modified.go, got %s", m.displayItems[3].change.Path)
	}

	// Untracked header
	if !m.displayItems[4].isHeader || m.displayItems[4].header != "Untracked (1)" {
		t.Errorf("expected Untracked header, got %+v", m.displayItems[4])
	}
	if m.displayItems[5].change.Path != "new.go" {
		t.Errorf("expected new.go, got %s", m.displayItems[5].change.Path)
	}
}

func TestRebuildDisplayListUntrackedOnlyShowsUntrackedSection(t *testing.T) {
	m := New()
	m.gitStatus = &git.StatusResult{
		Untracked: []git.Change{
			{Path: "a.go", Kind: git.ChangeUntracked},
			{Path: "b.go", Kind: git.ChangeUntracked},
		},
	}

	m.rebuildDisplayList()

	// Expect: Untracked header + 2 files
	if len(m.displayItems) != 3 {
		t.Fatalf("expected 3 display items, got %d", len(m.displayItems))
	}
	if !m.displayItems[0].isHeader || m.displayItems[0].header != "Untracked (2)" {
		t.Errorf("expected Untracked (2) header, got %+v", m.displayItems[0])
	}
}

func TestRebuildDisplayListUnstagedOnlyShowsUnstagedSection(t *testing.T) {
	m := New()
	m.gitStatus = &git.StatusResult{
		Unstaged: []git.Change{
			{Path: "changed.go", Kind: git.ChangeModified},
		},
	}

	m.rebuildDisplayList()

	// Expect: Unstaged header + 1 file, no Untracked section
	if len(m.displayItems) != 2 {
		t.Fatalf("expected 2 display items, got %d", len(m.displayItems))
	}
	if !m.displayItems[0].isHeader || m.displayItems[0].header != "Unstaged (1)" {
		t.Errorf("expected Unstaged (1) header, got %+v", m.displayItems[0])
	}
}
