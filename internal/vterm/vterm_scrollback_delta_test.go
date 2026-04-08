package vterm

import "testing"

func TestAppendScrollbackDeltaWithSize_MatchesRetainedSuffixAfterTrim(t *testing.T) {
	vt := New(20, 2)
	vt.LoadPaneCapture([]byte("zero\none\ntwo\nthree\nscreen one\nscreen two\n"))
	vt.Scrollback = append([][]Cell(nil), vt.Scrollback[2:]...)

	vt.AppendScrollbackDeltaWithSize([]byte("zero\none\ntwo\nthree\nscreen one\n"), 20, 2, 0)

	if len(vt.Scrollback) != 3 {
		t.Fatalf("expected retained suffix match to append only the missing row, got %d lines", len(vt.Scrollback))
	}
	if got := vt.Scrollback[0][0].Rune; got != 't' {
		t.Fatalf("expected trimmed suffix to start with two, got %q", got)
	}
	if got := vt.Scrollback[1][0].Rune; got != 't' {
		t.Fatalf("expected trimmed suffix to keep three, got %q", got)
	}
	if got := vt.Scrollback[2][0].Rune; got != 's' {
		t.Fatalf("expected missing scrolled row to append after trimmed suffix, got %q", got)
	}
}

func TestAppendScrollbackDeltaWithSize_PrefersScreenAlignedRepeatedMatch(t *testing.T) {
	vt := New(20, 2)
	vt.LoadPaneCapture([]byte("prompt\ncmd output\nprompt\n"))

	vt.AppendScrollbackDeltaWithSize([]byte("prompt\ncmd output\nprompt\n"), 20, 2, 0)

	if len(vt.Scrollback) != 3 {
		t.Fatalf("expected repeated-row capture to append the missing middle and tail rows, got %d lines", len(vt.Scrollback))
	}
	if got := vt.Scrollback[0][0].Rune; got != 'p' {
		t.Fatalf("expected retained prompt to stay first, got %q", got)
	}
	if got := vt.Scrollback[1][0].Rune; got != 'c' {
		t.Fatalf("expected screen-aligned middle row to reconcile into history, got %q", got)
	}
	if got := vt.Scrollback[2][0].Rune; got != 'p' {
		t.Fatalf("expected repeated trailing prompt to reconcile after the middle row, got %q", got)
	}
}

func TestAppendScrollbackDeltaWithSize_DropsVisibleTailAfterViewportGrowth(t *testing.T) {
	vt := New(20, 2)
	vt.LoadPaneCapture([]byte("history\nscreen one\nscreen two\n"))
	vt.Resize(20, 3)

	vt.AppendScrollbackDeltaWithSize([]byte("history\nscreen one\nscreen two\n"), 20, 2, 1)

	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected rows that remain visible after growth to stay off scrollback, got %d lines", len(vt.Scrollback))
	}
	if got := vt.Screen[0][0].Rune; got != 'h' {
		t.Fatalf("expected grown viewport to keep the revealed history row visible, got %q", got)
	}
}

func TestAppendScrollbackDeltaWithSize_PrefersLatestRepeatedRetainedSuffix(t *testing.T) {
	vt := New(20, 2)
	vt.LoadPaneCapture([]byte("A\nB\nscreen one\nscreen two\n"))
	vt.Scrollback = append([][]Cell(nil), vt.Scrollback[:2]...)

	vt.AppendScrollbackDeltaWithSize([]byte("A\nB\nA\nB\n"), 20, 2, 0)

	if len(vt.Scrollback) != 2 {
		t.Fatalf("expected repeated retained suffix to avoid duplicating existing history, got %d lines", len(vt.Scrollback))
	}
	if got := vt.Scrollback[0][0].Rune; got != 'A' {
		t.Fatalf("expected first retained row to remain A, got %q", got)
	}
	if got := vt.Scrollback[1][0].Rune; got != 'B' {
		t.Fatalf("expected second retained row to remain B, got %q", got)
	}
}
