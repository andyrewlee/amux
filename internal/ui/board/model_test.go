package board

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
)

func TestBoardNavigation(t *testing.T) {
	m := New()
	m.Focus()
	m.SetColumns([]BoardColumn{
		{Name: "Todo", Cards: []IssueCard{{IssueID: "1"}, {IssueID: "2"}}},
		{Name: "Done", Cards: []IssueCard{{IssueID: "3"}}},
	})

	// Move down
	m, _ = m.Update(tea.KeyPressMsg{Code: 'j'})
	if m.Selection.Row != 1 {
		t.Fatalf("expected row 1, got %d", m.Selection.Row)
	}

	// Move right
	m, _ = m.Update(tea.KeyPressMsg{Code: 'l'})
	if m.Selection.Column != 1 {
		t.Fatalf("expected column 1, got %d", m.Selection.Column)
	}
	if m.Selection.Row != 0 {
		t.Fatalf("expected row 0 after column change, got %d", m.Selection.Row)
	}
}

func TestBoardBadgeAction(t *testing.T) {
	m := New()
	card := IssueCard{IssueID: "1", Badges: []string{"CHANGES"}}
	cmd := m.badgeAction(card, 0)
	msg := cmd()
	if _, ok := msg.(messages.OpenIssueDiff); !ok {
		t.Fatalf("expected OpenIssueDiff, got %T", msg)
	}

	card = IssueCard{IssueID: "2", Badges: []string{"PR"}, PRURL: "https://example.com/pr/1"}
	cmd = m.badgeAction(card, 0)
	msg = cmd()
	if open, ok := msg.(messages.OpenURL); !ok || open.URL != "https://example.com/pr/1" {
		t.Fatalf("expected OpenURL to PR, got %T %#v", msg, msg)
	}

	card = IssueCard{IssueID: "3", Badges: []string{"RUNNING"}}
	cmd = m.badgeAction(card, 0)
	msg = cmd()
	if _, ok := msg.(messages.IssueSelected); !ok {
		t.Fatalf("expected IssueSelected, got %T", msg)
	}
}
