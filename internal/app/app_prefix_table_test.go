package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// prefixKeyMsg builds the KeyPressMsg a single-character prefix token
// produces — the spelling handlePrefixCommand's prefixInputToken expects.
func prefixKeyMsg(token string) tea.KeyPressMsg {
	r := []rune(token)
	if len(r) != 1 {
		return tea.KeyPressMsg{}
	}
	return tea.KeyPressMsg{Code: r[0], Text: token}
}

// TestPrefixCommands_MinimalAppMatchesStaticTable pins the conditional-row
// contract: with no scrollable center terminal, prefixCommands() is exactly
// prefixCommandTable — no injected scroll rows, no repurposed bindings.
func TestPrefixCommands_MinimalAppMatchesStaticTable(t *testing.T) {
	app := &App{}
	if app.centerScrollPrefixActive() {
		t.Fatal("minimal app unexpectedly has an active center scroll prefix")
	}
	produced := app.prefixCommands()
	if len(produced) != len(prefixCommandTable) {
		t.Fatalf("prefixCommands() produced %d rows, want %d (static table); "+
			"if conditional rows changed, update this table", len(produced), len(prefixCommandTable))
	}
	for i, row := range prefixCommandTable {
		if produced[i].Action != row.Action || produced[i].Desc != row.Desc {
			t.Fatalf("row %d (%q): produced action=%q desc=%q, want action=%q desc=%q",
				i, row.Sequence, produced[i].Action, produced[i].Desc, row.Action, row.Desc)
		}
	}
}

// TestPrefixTable_SequencesResolveToUniqueLeaf is the completeness net the
// table header comment warns about: every produced row's sequence must
// resolve to exactly that row, and driving the sequence must reach
// prefixMatchComplete. A rebound action, a dropped row, or a shadowed
// sequence fails here instead of silently no-oping at runtime.
func TestPrefixTable_SequencesResolveToUniqueLeaf(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40, Tabs: 3})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	app := h.app
	// Harness center tabs carry real vterm terminals, so the scroll prefix
	// is active here — the conditional `u` row and the repurposed `d` row
	// are part of the produced set this test drives.
	if !app.centerScrollPrefixActive() {
		t.Fatal("harness center expected an active scroll prefix; conditional rows are untested otherwise")
	}

	produced := app.prefixCommands()
	for _, row := range produced {
		matches := app.matchingPrefixCommands(row.Sequence)
		if len(matches) != 1 {
			t.Fatalf("prefix sequence %q resolved to %d rows; if you added a command, "+
				"keep sequences prefix-free", row.Sequence, len(matches))
		}
		if matches[0].Action != row.Action {
			t.Fatalf("prefix sequence %q resolved to action %q, want %q",
				row.Sequence, matches[0].Action, row.Action)
		}

		for i, token := range row.Sequence {
			status, _ := app.handlePrefixCommand(prefixKeyMsg(token))
			want := prefixMatchPartial
			if i == len(row.Sequence)-1 {
				want = prefixMatchComplete
			}
			if status != want {
				t.Fatalf("prefix sequence %q token %d (%q): got match state %v, want %v",
					row.Sequence, i, token, status, want)
			}
		}
		app.prefixSequence = nil
	}
}

// TestPrefixTable_StaticRowsPresent asserts every documented static-table
// sequence still appears in the produced command list — the scroll-active
// repurpose of `d` keeps its sequence, so presence (not action) is the
// contract that survives conditional mode.
func TestPrefixTable_StaticRowsPresent(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40, Tabs: 3})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	app := h.app
	produced := app.prefixCommands()
	for _, want := range prefixCommandTable {
		found := false
		for _, got := range produced {
			if len(got.Sequence) != len(want.Sequence) {
				continue
			}
			same := true
			for i := range want.Sequence {
				if got.Sequence[i] != want.Sequence[i] {
					same = false
					break
				}
			}
			if same {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("static row %q (%s) missing from prefixCommands(); "+
				"if you removed a command, delete it from prefixCommandTable and README.md",
				want.Sequence, want.Desc)
		}
	}
}

// TestPrefixTable_DigitRows pin the `1`…`9` tab-jump branch: each digit is a
// complete sequence whether or not a tab exists at that index.
func TestPrefixTable_DigitRows(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40, Tabs: 3})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	app := h.app
	for d := '1'; d <= '9'; d++ {
		token := string(d)
		status, _ := app.handlePrefixCommand(prefixKeyMsg(token))
		if status != prefixMatchComplete {
			t.Fatalf("digit jump %q: got match state %v, want complete", token, status)
		}
		app.prefixSequence = nil
	}
}
