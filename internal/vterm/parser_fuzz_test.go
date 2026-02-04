package vterm

import (
	"testing"
	"unicode/utf8"
)

func FuzzANSIParser(f *testing.F) {
	f.Add([]byte("hello"))
	f.Add([]byte("\x1b[31mred\x1b[0m"))
	f.Add([]byte("\x1b[?1049h\x1b[H\x1b[2J"))
	f.Fuzz(func(t *testing.T, data []byte) {
		vt := New(80, 24)
		p := NewParser(vt)
		p.Parse(data)
	})
}

func FuzzRenderInvariant(f *testing.F) {
	f.Add([]byte("line1\nline2"))
	f.Add([]byte("\x1b[1mBold\x1b[0m"))
	f.Add([]byte("\x1b]0;title\x07"))
	f.Fuzz(func(t *testing.T, data []byte) {
		vt := New(80, 24)
		vt.Write(data)
		out := vt.Render()
		if !utf8.ValidString(out) {
			t.Fatalf("render output is not valid utf-8")
		}
	})
}
