package center

import (
	"strings"
	"testing"
)

func TestPtyTraceFileName(t *testing.T) {
	cases := []struct {
		assistant  string
		wantPrefix string
	}{
		{"claude", "amux-pty-claude-"},
		{"codex", "amux-pty-codex-"},
		{"Cline", "amux-pty-cline-"},
		{"  gemini  ", "amux-pty-gemini-"},
		{"open code", "amux-pty-open-code-"},
		{"a/b\\c", "amux-pty-a-b-c-"},
		{"", "amux-pty-agent-"},
	}
	for _, c := range cases {
		got := ptyTraceFileName(c.assistant, "tab-1", "20060102-150405")
		if !strings.HasPrefix(got, c.wantPrefix) {
			t.Errorf("ptyTraceFileName(%q): got %q, want prefix %q", c.assistant, got, c.wantPrefix)
		}
		if !strings.HasSuffix(got, "-tab-1-20060102-150405.log") {
			t.Errorf("ptyTraceFileName(%q): unexpected suffix in %q", c.assistant, got)
		}
	}
}
