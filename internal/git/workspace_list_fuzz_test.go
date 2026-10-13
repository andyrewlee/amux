package git

import (
	"strings"
	"testing"
)

// FuzzParseWorktreeList exercises the `git worktree list --porcelain`
// parser — input surface that a bundled .git admin directory can forge.
// The parser must never panic, hang, or emit a path/branch the input did
// not literally contain.
func FuzzParseWorktreeList(f *testing.F) {
	f.Add("")
	f.Add("worktree /repo\nbranch refs/heads/main\n\n")
	f.Add("worktree /path with spaces\n\n")
	f.Add("worktree /bare\nbare\n\n")
	f.Add("worktree /detached\nHEAD abcdef\ndetached\n\n")
	f.Add("worktree /a\nbranch refs/heads/feat\n\nworktree /b\nbare\n\nworktree /c\nbranch refs/heads/x\n")
	f.Add("worktree \n\n")                                   // empty path
	f.Add("worktree /x\nbranch \n\n")                        // empty ref
	f.Add("\x00\x00worktree\x00\xff\n\n")                    // embedded NUL + invalid UTF-8
	f.Add("worktree /n\n\r\n")                               // CRLF line endings
	f.Add("worktree " + strings.Repeat("/p", 4096) + "\n\n") // long path
	f.Fuzz(func(t *testing.T, data string) {
		got := parseWorktreeList(data, "/repo")
		// Output cannot exceed input line count (no amplification).
		if n := strings.Count(data, "\n") + 1; len(got) > n {
			t.Fatalf("amplified: %d workspaces from %d lines", len(got), n)
		}
		for _, ws := range got {
			if ws.Root == "" {
				t.Fatalf("empty root emitted for input %q", data)
			}
			if ws.Name == "" {
				t.Fatalf("empty name emitted for input %q", data)
			}
			if ws.Repo != "/repo" {
				t.Fatalf("repo %q emitted, want the passed repoPath", ws.Repo)
			}
			// Roots must come literally from a `worktree ` line payload —
			// the parser only strips a trailing \r, never rewrites paths.
			if !strings.Contains(data, ws.Root) {
				t.Fatalf("root %q not present in input %q", ws.Root, data)
			}
		}
	})
}
