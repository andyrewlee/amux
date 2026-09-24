package ptyio

import "testing"

func TestFilterKnownPTYNoise_RemovesMacOSMallocDiagnosticLine(t *testing.T) {
	in := []byte("hello\r\ncodex(32758,0x16f58f000) malloc: nano zone abandoned\r\nworld\r\n")
	got := FilterKnownPTYNoiseStream(in, nil)
	want := "hello\r\nworld\r\n"
	if string(got) != want {
		t.Fatalf("filtered output = %q, want %q", string(got), want)
	}
}

func TestFilterKnownPTYNoise_RemovesUppercaseMallocPrefix(t *testing.T) {
	in := []byte("codex(32758) Malloc: debugging enabled\n")
	got := FilterKnownPTYNoiseStream(in, nil)
	if len(got) != 0 {
		t.Fatalf("expected diagnostic line to be removed, got %q", string(got))
	}
}

func TestFilterKnownPTYNoise_KeepsNormalOutput(t *testing.T) {
	in := []byte("> codex review malloc issue\n")
	got := FilterKnownPTYNoiseStream(in, nil)
	if string(got) != string(in) {
		t.Fatalf("normal output was modified: got %q want %q", string(got), string(in))
	}
}

func TestFilterKnownPTYNoise_KeepsNonDiagnosticMallocMentions(t *testing.T) {
	in := []byte("alloc_report(42) mallocz: custom allocator stats\n")
	got := FilterKnownPTYNoiseStream(in, nil)
	if string(got) != string(in) {
		t.Fatalf("non-diagnostic line was removed: got %q want %q", string(got), string(in))
	}
}

func TestFilterKnownPTYNoise_KeepsIncompleteTrailingDiagnosticFragment(t *testing.T) {
	in := []byte("prefix\ncodex(32758,0x16f58f000) malloc: nano zone")
	got := FilterKnownPTYNoiseStream(in, nil)
	if string(got) != string(in) {
		t.Fatalf("incomplete trailing fragment was modified: got %q want %q", string(got), string(in))
	}
}

func TestFilterKnownPTYNoise_RemovesMixedCaseMallocPrefix(t *testing.T) {
	in := []byte("codex(32758) mAlLoC: debugging enabled\n")
	got := FilterKnownPTYNoiseStream(in, nil)
	if len(got) != 0 {
		t.Fatalf("expected mixed-case diagnostic line to be removed, got %q", string(got))
	}
}

func TestFilterKnownPTYNoise_RemovesDiagnosticLineWithANSI(t *testing.T) {
	in := []byte("\x1b[31mcodex(32758) malloc: debugging enabled\x1b[0m\n")
	got := FilterKnownPTYNoiseStream(in, nil)
	if len(got) != 0 {
		t.Fatalf("expected ANSI-styled diagnostic line to be removed, got %q", string(got))
	}
}

func TestFilterKnownPTYNoiseStream_RemovesSplitDiagnosticLine(t *testing.T) {
	var trailing []byte
	got1 := FilterKnownPTYNoiseStream([]byte("hello\nagent(32758) malloc: nano zone"), &trailing)
	if string(got1) != "hello\n" {
		t.Fatalf("first chunk filtered output = %q, want %q", string(got1), "hello\n")
	}
	if len(trailing) == 0 {
		t.Fatalf("expected trailing diagnostic fragment to be buffered")
	}

	got2 := FilterKnownPTYNoiseStream([]byte(" abandoned\nworld\n"), &trailing)
	if string(got2) != "world\n" {
		t.Fatalf("second chunk filtered output = %q, want %q", string(got2), "world\n")
	}
	if len(trailing) != 0 {
		t.Fatalf("expected buffered fragment to be consumed, got %q", string(trailing))
	}
}

func TestFilterKnownPTYNoiseStream_KeepsNormalIncompleteLine(t *testing.T) {
	var trailing []byte
	in := []byte("progress 50%")
	got := FilterKnownPTYNoiseStream(in, &trailing)
	if string(got) != string(in) {
		t.Fatalf("normal incomplete fragment was modified: got %q want %q", string(got), string(in))
	}
	if len(trailing) != 0 {
		t.Fatalf("did not expect trailing buffer for normal fragment, got %q", string(trailing))
	}
}

func TestFilterKnownPTYNoiseStream_ReleasesBufferedNonDiagnosticPrefix(t *testing.T) {
	var trailing []byte
	got1 := FilterKnownPTYNoiseStream([]byte("agent(32758) "), &trailing)
	if len(got1) != 0 {
		t.Fatalf("expected ambiguous prefix to be buffered, got %q", string(got1))
	}
	if len(trailing) == 0 {
		t.Fatalf("expected buffered prefix")
	}

	got2 := FilterKnownPTYNoiseStream([]byte("status update\n"), &trailing)
	if string(got2) != "agent(32758) status update\n" {
		t.Fatalf("expected buffered prefix to flush on next chunk, got %q", string(got2))
	}
	if len(trailing) != 0 {
		t.Fatalf("expected trailing buffer to be cleared, got %q", string(trailing))
	}
}

func TestFilterKnownPTYNoiseStream_RemovesSplitDiagnosticLineWithANSI(t *testing.T) {
	var trailing []byte
	got1 := FilterKnownPTYNoiseStream([]byte("\x1b[31magent(32758) malloc: nano"), &trailing)
	if len(got1) != 0 {
		t.Fatalf("expected ANSI diagnostic fragment to be buffered, got %q", string(got1))
	}
	got2 := FilterKnownPTYNoiseStream([]byte(" zone abandoned\x1b[0m\nok\n"), &trailing)
	if string(got2) != "ok\n" {
		t.Fatalf("expected split ANSI diagnostic line removal, got %q", string(got2))
	}
}

func TestDrainKnownPTYNoiseTrailing(t *testing.T) {
	var trailing []byte
	trailing = append(trailing, "agent(32758) "...)
	got := DrainKnownPTYNoiseTrailing(&trailing)
	if string(got) != "agent(32758) " {
		t.Fatalf("expected drained bytes, got %q", string(got))
	}
	if len(trailing) != 0 {
		t.Fatalf("expected trailing buffer to be cleared, got %q", string(trailing))
	}
}

func TestIsProcessToken_RejectsEmpty(t *testing.T) {
	if isProcessToken(nil) {
		t.Fatal("expected empty token to be rejected")
	}
	if isProcessToken([]byte{}) {
		t.Fatal("expected empty token slice to be rejected")
	}
}

// TestContainsASCIIFold_IndexSeededEquivalence pins the seeded-scan
// implementation against the semantics of the naive per-position fold: every
// case here was hand-verified against the old loop.
func TestContainsASCIIFold_IndexSeededEquivalence(t *testing.T) {
	tests := []struct {
		name     string
		haystack []byte
		needle   string
		want     bool
	}{
		{"empty needle", []byte("anything"), "", true},
		{"empty haystack", nil, "malloc", false},
		{"shorter than needle", []byte("mall"), "malloc", false},
		{"exact lowercase", []byte("malloc"), "malloc", true},
		{"exact uppercase", []byte("MALLOC"), "malloc", true},
		{"mixed case", []byte("MaLlOc"), "malloc", true},
		{"prefix only", []byte("mall"), "malloc", false},
		{"embedded", []byte("xx malloc: boom yy"), "malloc", true},
		{"embedded mixed", []byte("xx Malloc: boom"), "malloc", true},
		{"needle at tail", []byte("diag ends with MALLOC"), "malloc", true},
		{"needle at head", []byte("malloc starts"), "malloc", true},
		{"adjacent partials", []byte("mal mal loc"), "malloc", false},
		{"repeated first byte", []byte("mmmmmm"), "malloc", false},
		{"first-byte run then hit", []byte("m m ma malloc"), "malloc", true},
		{"overlap retry", []byte("mamalloc"), "malloc", true},
		{"non-letter needle", []byte("a=b;c=d"), "=d", true},
		{"non-letter miss", []byte("a=b;c=d"), "=e", false},
		{"uppercase needle no fold-up", []byte("malloc"), "MALLOC", false},
		{"uppercase needle never matches", []byte("MALLOC"), "MALLOC", false},
		{"single char needle", []byte("xMyx"), "m", true},
		{"needle past end", []byte("mallo"), "malloc", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsASCIIFold(tt.haystack, tt.needle); got != tt.want {
				t.Fatalf("containsASCIIFold(%q, %q) = %v, want %v", tt.haystack, tt.needle, got, tt.want)
			}
		})
	}
}
