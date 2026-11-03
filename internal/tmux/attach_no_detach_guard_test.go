package tmux

import (
	"os"
	"strings"
	"testing"
)

// TestNoDetachingAttachAnywhere pins the multi-instance contract at the
// source level: this package is the single place tmux argv and client shell
// commands are built, and NO file in it may construct a detaching attach —
// in shell strings (`attach -d`, `attach -dt`), in adjacent argv literals
// (`"attach-session", "-d"`), or in the spliced argv form this package
// actually uses (`append(args, "-d")` after an attach construction — the
// idiomatic shape, and the exact shape this change's own tests build for
// the red control). A detaching attach anywhere starts a detach war between
// amux instances sharing a workspace (each victim's restart loop re-attaches
// and kicks the other back), and even a guarded one loses the race against a
// concurrent creator whose fresh session is briefly client-free.
//
// Deliberately NOT forbidden: `-d` after `new-session` (detached creation is
// required, not a client eviction), and a `-d` assembled from variables is
// beyond a source scan — the behavioral tests are the backstop there.
//
// The behavioral halves are pinned separately: command_test.go and
// tmux_test.go assert the rendered NewClientCommand/AttachOnlyClientCommand
// strings contain plain `attach -t` and never `attach -dt`, and
// attach_share_integration_test.go proves against a real tmux server that
// the plain form shares while the -d form evicts. This guard closes the
// remaining hole: a future THIRD renderer emitting -d directly would pass
// those tests and still restart the war.
//
// Same convention as internal/process's TestNoDirectSlogUsage: a source-scan
// of the package's non-test files, so the failure names the offending file.
func TestNoDetachingAttachAnywhere(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			forbidden := []string{
				`attach -d`, `attach -dt`, // shell-string forms
				`"attach-session", "-d"`, `"attach", "-d"`, // adjacent argv literals
				`append(args, "-d"`, `append(args, "-dt"`, // spliced argv forms
			}
			for _, pattern := range forbidden {
				if strings.Contains(line, pattern) {
					t.Errorf("%s:%d constructs a detaching attach (%q) — amux must never emit attach -d; see attach_share_integration_test.go for the war this causes", name, i+1, strings.TrimSpace(line))
				}
			}
		}
	}
}
