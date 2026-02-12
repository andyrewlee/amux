package git

import "testing"

func FuzzParseStatusPorcelain(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("?? foo\x00"))
	f.Add([]byte(" M bar\x00"))
	f.Add([]byte("R  new.txt\x00old.txt\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_ = parseStatusPorcelain(data)
	})
}
