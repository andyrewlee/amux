package cli

import "testing"

func TestBuildExecShellCommandQuotesEachArgument(t *testing.T) {
	got := buildExecShellCommand("/tmp/my dir", []string{"printf", "%s\\n", "a b", "x;rm -rf /", "a'b"})
	want := "cd '/tmp/my dir' && 'printf' '%s\\n' 'a b' 'x;rm -rf /' 'a'\\''b'"
	if got != want {
		t.Fatalf("buildExecShellCommand() = %q, want %q", got, want)
	}
}
