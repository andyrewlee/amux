package vterm

import (
	"strings"
	"testing"
)

func TestRenderSuppressesUnderlineOnBlankCells(t *testing.T) {
	t.Parallel()
	vt := New(5, 1)
	vt.CurrentStyle.Underline = true
	vt.Write([]byte("     "))

	out := vt.Render()
	if ContainsSGRParam(out, 4) {
		t.Fatalf("expected no underline SGR for blank cells, got %q", out)
	}
}

func TestStyleToDeltaANSIPreservesBoldWhenTurningOffDim(t *testing.T) {
	t.Parallel()
	prev := Style{Bold: true, Dim: true}
	next := Style{Bold: true}
	out := prev.DeltaANSI(next)
	if !strings.Contains(out, "22") || !strings.Contains(out, "1") {
		t.Fatalf("expected delta to disable dim and preserve bold, got %q", out)
	}
}

func TestRenderKeepsUnderlineForText(t *testing.T) {
	t.Parallel()
	vt := New(2, 1)
	vt.CurrentStyle.Underline = true
	vt.Write([]byte("A "))

	out := vt.Render()
	if !ContainsSGRParam(out, 4) {
		t.Fatalf("expected underline SGR for text, got %q", out)
	}
}
