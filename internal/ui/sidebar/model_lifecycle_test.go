package sidebar

import (
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/git"
)

func TestSetGitStatusFastResultDoesNotPreserveOldLineStats(t *testing.T) {
	m := New()
	m.SetGitStatus(&git.StatusResult{
		Clean:        false,
		Unstaged:     []git.Change{{Path: "README.md"}},
		TotalAdded:   12,
		TotalDeleted: 3,
		HasLineStats: true,
	})
	m.SetGitStatus(&git.StatusResult{
		Clean:        false,
		Unstaged:     []git.Change{{Path: "README.md"}},
		HasLineStats: false,
	})

	if m.gitStatus == nil {
		t.Fatal("expected git status to be set")
	}
	if m.gitStatus.TotalAdded != 0 || m.gitStatus.TotalDeleted != 0 {
		t.Fatalf("expected fast status to keep zero totals, got +%d -%d", m.gitStatus.TotalAdded, m.gitStatus.TotalDeleted)
	}
	if m.gitStatus.HasLineStats {
		t.Fatal("expected HasLineStats=false for fast status")
	}
}

func TestRenderBodyHidesLineTotalsWhenStatsUnknown(t *testing.T) {
	m := New()
	m.SetSize(80, 20)
	m.SetGitStatus(&git.StatusResult{
		Clean:        false,
		Unstaged:     []git.Change{{Path: "README.md"}},
		TotalAdded:   12,
		TotalDeleted: 3,
		HasLineStats: false,
	})

	body := m.renderChanges()
	if strings.Contains(body, "+12") || strings.Contains(body, "-3") {
		t.Fatalf("expected line totals to be hidden when stats are unknown, body=%q", body)
	}
}
