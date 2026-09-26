package common

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// keyMsg builds the real key message Update sees for a navigation key.
func keyMsg(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

// enterResult sends Enter and returns the emitted DialogResult.
func enterResult(t *testing.T, d *Dialog) DialogResult {
	t.Helper()
	_, cmd := d.Update(keyMsg(tea.KeyEnter, 0))
	if cmd == nil {
		t.Fatal("enter produced no result")
	}
	res, ok := cmd().(DialogResult)
	if !ok {
		t.Fatalf("enter result = %T, want DialogResult", cmd())
	}
	return res
}

// TestDialogNavigation_StructuralKeysOnFilteredSelect pins the plan-036
// contract: arrows, Tab, and Shift+Tab navigate a filtered select — they are
// structural keys, not filter text — with wraparound at both ends.
func TestDialogNavigation_StructuralKeysOnFilteredSelect(t *testing.T) {
	tests := []struct {
		name  string
		keys  []tea.KeyPressMsg
		start int
		want  int
	}{
		{name: "down", keys: []tea.KeyPressMsg{keyMsg(tea.KeyDown, 0)}, want: 1},
		{name: "tab", keys: []tea.KeyPressMsg{keyMsg(tea.KeyTab, 0)}, want: 1},
		{name: "up wraps to last", keys: []tea.KeyPressMsg{keyMsg(tea.KeyUp, 0)}, want: 2},
		{name: "shift+tab wraps to last", keys: []tea.KeyPressMsg{keyMsg(tea.KeyTab, tea.ModShift)}, want: 2},
		{name: "down wraps to first", keys: []tea.KeyPressMsg{keyMsg(tea.KeyDown, 0), keyMsg(tea.KeyDown, 0), keyMsg(tea.KeyDown, 0)}, want: 0},
		{name: "tab past end wraps", keys: []tea.KeyPressMsg{keyMsg(tea.KeyTab, 0), keyMsg(tea.KeyTab, 0), keyMsg(tea.KeyTab, 0), keyMsg(tea.KeyTab, 0)}, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewAgentPicker([]string{"claude", "codex", "gemini"})
			d.SetSize(100, 40)
			d.Show()
			for _, k := range tt.keys {
				var cmd tea.Cmd
				d, cmd = d.Update(k)
				if cmd != nil {
					t.Fatal("navigation must not emit a result command")
				}
			}
			if d.cursor != tt.want {
				t.Fatalf("cursor = %d, want %d", d.cursor, tt.want)
			}
			if d.filterInput.Value() != "" {
				t.Fatalf("structural key leaked into filter input: %q", d.filterInput.Value())
			}
		})
	}
}

// TestDialogNavigation_FilteredEnterTranslatesToOriginalIndex covers the
// filtered-index → original-index mapping after navigation.
func TestDialogNavigation_FilteredEnterTranslatesToOriginalIndex(t *testing.T) {
	d := NewAgentPicker([]string{"claude", "codex", "gemini"})
	d.SetSize(100, 40)
	d.Show()

	// Filter to a single match ("codex"), then move with Tab — clamped to the
	// one filtered row — and Enter must report the ORIGINAL index 1.
	d, _ = d.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	d, _ = d.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	if got := len(d.filteredIndices); got != 1 {
		t.Fatalf("filtered indices = %d, want 1", got)
	}
	d, _ = d.Update(keyMsg(tea.KeyTab, 0))
	res := enterResult(t, d)
	if !res.Confirmed || res.Index != 1 || res.Value != "codex" {
		t.Fatalf("result = %+v, want confirmed index 1 codex", res)
	}
}

// TestDialogNavigation_PrintableKeysReachFilter asserts j/k type into the
// filter on a filtered select instead of navigating.
func TestDialogNavigation_PrintableKeysReachFilter(t *testing.T) {
	d := NewAgentPicker([]string{"ja", "jb", "kc"})
	d.SetSize(100, 40)
	d.Show()

	d, _ = d.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := d.filterInput.Value(); got != "j" {
		t.Fatalf("filter = %q, want typed j", got)
	}
	if d.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (j filtered, not navigated)", d.cursor)
	}
	// Two remaining matches both start with j; cursor still selects filtered 0.
	if got := len(d.filteredIndices); got != 2 {
		t.Fatalf("filtered = %d, want 2", got)
	}
	res := enterResult(t, d)
	if res.Value != "ja" {
		t.Fatalf("selected %q, want ja", res.Value)
	}
}

// TestDialogNavigation_NoMatchAndSingleMatch covers the degenerate filtered
// states: navigation must not panic or move, and Enter must not confirm.
func TestDialogNavigation_NoMatchAndSingleMatch(t *testing.T) {
	d := NewAgentPicker([]string{"claude", "codex"})
	d.SetSize(100, 40)
	d.Show()
	for _, r := range "zzz" {
		d, _ = d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if len(d.filteredIndices) != 0 {
		t.Fatal("expected zero matches")
	}
	// Structural keys on an empty result set: no panic, no result.
	var cmd tea.Cmd
	d, cmd = d.Update(keyMsg(tea.KeyDown, 0))
	if cmd != nil {
		t.Fatal("navigation on empty filter emitted a result")
	}
	if _, cmd = d.Update(keyMsg(tea.KeyEnter, 0)); cmd != nil {
		if res, ok := cmd().(DialogResult); ok && res.Confirmed {
			t.Fatal("enter confirmed with zero matches")
		}
	}
}

// TestDialogNavigation_InputDialogPrintable covers j/k inside a DialogInput —
// they are text, never navigation.
func TestDialogNavigation_InputDialogPrintable(t *testing.T) {
	d := NewInputDialog("agent-input", "Title", "type here")
	d.Show()
	d, _ = d.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	d, _ = d.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if d.input.Value() != "jk" {
		t.Fatalf("input = %q, want jk", d.input.Value())
	}
	res := enterResult(t, d)
	if res.Value != "jk" {
		t.Fatalf("result value = %q, want jk", res.Value)
	}
}

// TestDialogNavigation_UnfilteredSelectPrintableNav covers the unfiltered
// select: j/k navigate (run-session picker contract), arrows/tab do too.
func TestDialogNavigation_UnfilteredSelectPrintableNav(t *testing.T) {
	d := NewRunSessionPicker("t", "m", []SessionPickerRow{
		{Label: "one"}, {Label: "two"}, {Label: "three"},
	})
	d.SetSize(100, 40)
	d.Show()
	for _, k := range []tea.KeyPressMsg{
		{Code: 'j', Text: "j"},
		{Code: 'j', Text: "j"},
		keyMsg(tea.KeyUp, 0),
	} {
		var cmd tea.Cmd
		d, cmd = d.Update(k)
		if cmd != nil {
			t.Fatal("navigation emitted a result")
		}
	}
	if d.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", d.cursor)
	}
}
