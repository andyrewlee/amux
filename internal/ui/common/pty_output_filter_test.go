package common

import "testing"

func TestFilterKnownPTYNoise_RemovesMacOSMallocDiagnosticLine(t *testing.T) {
	in := []byte("hello\r\ncodex(32758,0x16f58f000) malloc: nano zone abandoned\r\nworld\r\n")
	got := FilterKnownPTYNoise(in)
	want := "hello\r\nworld\r\n"
	if string(got) != want {
		t.Fatalf("filtered output = %q, want %q", string(got), want)
	}
}

func TestFilterKnownPTYNoise_RemovesUppercaseMallocPrefix(t *testing.T) {
	in := []byte("codex(32758) Malloc: debugging enabled\n")
	got := FilterKnownPTYNoise(in)
	if len(got) != 0 {
		t.Fatalf("expected diagnostic line to be removed, got %q", string(got))
	}
}

func TestFilterKnownPTYNoise_KeepsNormalOutput(t *testing.T) {
	in := []byte("> codex review malloc issue\n")
	got := FilterKnownPTYNoise(in)
	if string(got) != string(in) {
		t.Fatalf("normal output was modified: got %q want %q", string(got), string(in))
	}
}

func TestFilterKnownPTYNoise_KeepsNonDiagnosticMallocMentions(t *testing.T) {
	in := []byte("alloc_report(42) mallocz: custom allocator stats\n")
	got := FilterKnownPTYNoise(in)
	if string(got) != string(in) {
		t.Fatalf("non-diagnostic line was removed: got %q want %q", string(got), string(in))
	}
}

func TestFilterKnownPTYNoise_KeepsIncompleteTrailingDiagnosticFragment(t *testing.T) {
	in := []byte("prefix\ncodex(32758,0x16f58f000) malloc: nano zone")
	got := FilterKnownPTYNoise(in)
	if string(got) != string(in) {
		t.Fatalf("incomplete trailing fragment was modified: got %q want %q", string(got), string(in))
	}
}
