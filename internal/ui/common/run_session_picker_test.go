package common

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func newTestSessionPicker() *Dialog {
	return NewRunSessionPicker("Run sessions — ws", "Pick a run:", []SessionPickerRow{
		{Label: "#1 — running", Live: true},
		{Label: "#2 — exited 7", Live: false},
		{Label: "#3 — exited 0", Live: false},
	})
}

func TestRunSessionPicker_RendersVerticalRows(t *testing.T) {
	d := newTestSessionPicker()
	d.SetSize(100, 40)
	d.Show()

	view := d.View()
	for _, row := range []string{"#1 — running", "#2 — exited 7", "#3 — exited 0"} {
		if !strings.Contains(view, row) {
			t.Fatalf("picker missing row %q, got:\n%s", row, view)
		}
	}
	// Vertical layout: each session row lands on its own line.
	lines := strings.Split(view, "\n")
	var rowLines int
	for _, l := range lines {
		if strings.Contains(l, "#1 —") || strings.Contains(l, "#2 —") || strings.Contains(l, "#3 —") {
			rowLines++
		}
	}
	if rowLines != 3 {
		t.Fatalf("expected 3 vertical rows, got %d:\n%s", rowLines, view)
	}
}

func TestRunSessionPicker_NavigationAndSelect(t *testing.T) {
	d := newTestSessionPicker()
	d.SetSize(100, 40)
	d.Show()

	// j moves down (unfiltered select navigation), k wraps back up.
	for _, k := range []string{"j", "j"} {
		var cmd tea.Cmd
		d, cmd = d.Update(tea.KeyPressMsg{Code: []rune(k)[0], Text: k})
		if cmd != nil {
			t.Fatal("navigation must not emit a result")
		}
	}
	var cmd tea.Cmd
	d, cmd = d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter produced no result")
	}
	res, ok := cmd().(DialogResult)
	if !ok {
		t.Fatalf("enter emitted %T, want DialogResult", cmd())
	}
	if !res.Confirmed || res.Index != 2 || res.Value != "#3 — exited 0" {
		t.Fatalf("result = %+v, want confirmed index 2", res)
	}
	if d.Visible() {
		t.Fatal("dialog still visible after selection")
	}
}

func TestRunSessionPicker_UpWraps(t *testing.T) {
	d := newTestSessionPicker()
	d.SetSize(100, 40)
	d.Show()

	var cmd tea.Cmd
	d, cmd = d.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if cmd != nil {
		t.Fatal("navigation must not emit a result")
	}
	if !d.Visible() {
		t.Fatal("navigation closed the dialog")
	}
	_, cmd = d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter produced no result")
	}
	res, ok := cmd().(DialogResult)
	if !ok {
		t.Fatalf("enter emitted %T, want DialogResult", cmd())
	}
	if res.Index != 2 {
		t.Fatalf("k from row 0 should wrap to last row (index 2), got %d", res.Index)
	}
}

func TestRunSessionPicker_EscCancels(t *testing.T) {
	d := newTestSessionPicker()
	d.SetSize(100, 40)
	d.Show()

	var cmd tea.Cmd
	d, cmd = d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("esc produced no result")
	}
	if d.Visible() {
		t.Fatal("dialog still visible after esc")
	}
	res, ok := cmd().(DialogResult)
	if !ok {
		t.Fatalf("esc emitted %T, want DialogResult", cmd())
	}
	if res.Confirmed {
		t.Fatal("esc must cancel")
	}
	if res.ID != RunSessionPickerDialogID {
		t.Fatalf("result ID = %q, want %q", res.ID, RunSessionPickerDialogID)
	}
}
