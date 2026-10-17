package git

import (
	"strings"
	"testing"
)

func FuzzParseStatusPorcelain(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("?? foo\x00"))
	f.Add([]byte(" M bar\x00"))
	f.Add([]byte("R  new.txt\x00old.txt\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_ = parseStatusPorcelain(data)
	})
}

// FuzzParseNameStatus exercises the `git diff --name-status -z` parser —
// input surface a crafted diff could forge. The parser must never panic,
// emit more changes than tokens, or produce a path the input did not
// literally contain.
func FuzzParseNameStatus(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("M\x00foo\x00"))
	f.Add([]byte("R100\x00old name\x00new name\x00"))
	f.Add([]byte("C99\x00src\x00dst\x00"))
	f.Add([]byte("A\x00p with\nnewline\x00"))
	f.Add([]byte("R1\x00truncated\x00"))
	f.Add([]byte("\x00\x00\xff\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		got := parseNameStatus(data)
		input := string(data)
		tokens := strings.Count(input, "\x00") + 1
		if len(got) > tokens {
			t.Fatalf("amplified: %d changes from %d tokens", len(got), tokens)
		}
		for _, c := range got {
			if c.Path == "" {
				t.Fatalf("empty path emitted for input %q", input)
			}
			if !strings.Contains(input, c.Path) {
				t.Fatalf("path %q not present in input %q", c.Path, input)
			}
			if c.OldPath != "" && !strings.Contains(input, c.OldPath) {
				t.Fatalf("old path %q not present in input %q", c.OldPath, input)
			}
		}
	})
}
