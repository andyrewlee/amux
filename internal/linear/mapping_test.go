package linear

import "testing"

func TestMapStateToColumnAuto(t *testing.T) {
	state := State{Type: "started"}
	col := MapStateToColumn(state, Team{}, BoardConfig{Columns: []string{"Todo", "In Progress", "In Review", "Done"}})
	if col != "In Progress" {
		t.Fatalf("expected In Progress, got %q", col)
	}
}

func TestMapStateToColumnCustom(t *testing.T) {
	cfg := BoardConfig{StateMapping: map[string]map[string]string{"TeamA": {"Queued": "Todo"}}}
	col := MapStateToColumn(State{Name: "Queued", Type: "backlog"}, Team{Name: "TeamA"}, cfg)
	if col != "Todo" {
		t.Fatalf("expected Todo, got %q", col)
	}
}

func TestColumnIndex(t *testing.T) {
	idx := ColumnIndex([]string{"Todo", "In Progress"}, "in progress")
	if idx != 1 {
		t.Fatalf("expected 1, got %d", idx)
	}
}
