package vterm

import (
	"strconv"
	"strings"
	"testing"
)

func TestRenderSuppressesUnderlineOnBlankCells(t *testing.T) {
	vt := New(5, 1)
	vt.CurrentStyle.Underline = true
	vt.Write([]byte("     "))

	out := vt.Render()
	if containsSGRParam(out, 4) {
		t.Fatalf("expected no underline SGR for blank cells, got %q", out)
	}
}

func TestStyleToDeltaANSIPreservesBoldWhenTurningOffDim(t *testing.T) {
	prev := Style{Bold: true, Dim: true}
	next := Style{Bold: true}
	out := StyleToDeltaANSI(prev, next)
	if !strings.Contains(out, "22") || !strings.Contains(out, "1") {
		t.Fatalf("expected delta to disable dim and preserve bold, got %q", out)
	}
}

func TestRenderKeepsUnderlineForText(t *testing.T) {
	vt := New(2, 1)
	vt.CurrentStyle.Underline = true
	vt.Write([]byte("A "))

	out := vt.Render()
	if !containsSGRParam(out, 4) {
		t.Fatalf("expected underline SGR for text, got %q", out)
	}
}

func containsSGRParam(s string, target int) bool {
	targetStr := strconv.Itoa(target)
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b || i+1 >= len(s) || s[i+1] != '[' {
			continue
		}
		j := i + 2
		for j < len(s) && s[j] != 'm' {
			j++
		}
		if j >= len(s) {
			break
		}
		params := strings.Split(s[i+2:j], ";")
		for _, param := range params {
			if param == targetStr {
				return true
			}
		}
		i = j
	}
	return false
}
