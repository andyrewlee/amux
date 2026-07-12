package center

import (
	"strings"
	"testing"

	"github.com/clipperhouse/displaywidth"

	"github.com/andyrewlee/amux/internal/messages"
)

// TestTruncateDisplayName exercises the display-width-based trailing-tail
// policy: names 20 cells or narrower are returned verbatim, while wider names
// are rewritten as "..." plus a suffix that fits the remaining cell budget.
func TestTruncateDisplayName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "short", input: "claude", want: "claude"},
		{
			name:  "exactly twenty kept verbatim",
			input: "12345678901234567890",
			want:  "12345678901234567890",
		},
		{
			name:  "twenty-one truncated to ellipsis plus 17-byte tail",
			input: "123456789012345678901",
			want:  "...56789012345678901",
		},
		{
			name:  "long name keeps trailing identifier",
			input: "this-is-a-very-long-agent-display-name",
			want:  "...gent-display-name",
		},
		{
			name:  "alphabet truncated",
			input: "abcdefghijklmnopqrstuvwxyz",
			want:  "...jklmnopqrstuvwxyz",
		},
		{
			name:  "multibyte name truncates on rune boundary",
			input: strings.Repeat("界", 21),
			want:  "..." + strings.Repeat("界", 8),
		},
		{
			name:  "twenty-cell multibyte name kept verbatim",
			input: strings.Repeat("界", 10),
			want:  strings.Repeat("界", 10),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateDisplayName(tt.input)
			if got != tt.want {
				t.Fatalf("truncateDisplayName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestTruncateDisplayName_BoundaryLengths is a property check across the cutover:
// the result is never wider than 20 cells, and a truncated result always begins
// with the "..." marker while preserving the original suffix.
func TestTruncateDisplayName_BoundaryLengths(t *testing.T) {
	for n := 0; n <= 40; n++ {
		input := strings.Repeat("a", n)
		// Tag the tail so truncation is observable distinctly from the body.
		if n >= 5 {
			input = input[:n-5] + "TAIL5"
		}

		got := truncateDisplayName(input)

		if len(input) <= 20 {
			if got != input {
				t.Fatalf("len=%d: expected verbatim %q, got %q", n, input, got)
			}
			continue
		}
		if gotWidth := displaywidth.String(got); gotWidth > 20 {
			t.Fatalf("len=%d: truncated result %q has display width %d, want <= 20", n, got, gotWidth)
		}
		if !strings.HasPrefix(got, "...") {
			t.Fatalf("len=%d: truncated result %q must start with ...", n, got)
		}
		// The trailing 17 bytes of the ASCII original survive verbatim.
		if want := input[len(input)-17:]; !strings.HasSuffix(got, want) {
			t.Fatalf("len=%d: truncated result %q must end with original tail %q", n, got, want)
		}
	}
}

// TestTruncateDisplayName_WideGlyphs confirms the tail-keeping policy holds for
// non-ASCII names whose grapheme clusters occupy two display cells each: an
// over-budget emoji or CJK name is rewritten to a "..."-prefixed suffix that
// never exceeds the 20-cell budget.
func TestTruncateDisplayName_WideGlyphs(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "emoji-heavy name", input: strings.Repeat("😀", 15)},
		{name: "cjk name", input: strings.Repeat("日本語", 8)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateDisplayName(tt.input)
			if w := displaywidth.String(got); w > 20 {
				t.Fatalf("truncateDisplayName(%q) width = %d, want <= 20", tt.input, w)
			}
			if !strings.HasPrefix(got, "...") {
				t.Fatalf("truncateDisplayName(%q) = %q, want a \"...\" prefix", tt.input, got)
			}
		})
	}
}

// TestNextAssistantName covers the de-duplication policy for tab display names:
// the first tab of an assistant uses the bare name, and subsequent tabs append
// an incrementing index that skips names already taken.
func TestNextAssistantName(t *testing.T) {
	tab := func(assistant, name string) *Tab {
		return &Tab{Assistant: assistant, Name: name}
	}

	tests := []struct {
		name      string
		assistant string
		tabs      []*Tab
		want      string
	}{
		{name: "empty assistant returns empty", assistant: "", tabs: nil, want: ""},
		{name: "whitespace-only assistant returns empty", assistant: "   ", tabs: nil, want: ""},
		{
			name:      "trims surrounding whitespace before matching",
			assistant: "  claude  ",
			tabs:      nil,
			want:      "claude",
		},
		{name: "no existing tabs uses bare name", assistant: "claude", tabs: nil, want: "claude"},
		{
			name:      "different assistant does not collide",
			assistant: "claude",
			tabs:      []*Tab{tab("codex", "codex")},
			want:      "claude",
		},
		{
			name:      "nil tab entries are skipped",
			assistant: "claude",
			tabs:      []*Tab{nil, tab("claude", "claude")},
			want:      "claude 1",
		},
		{
			name:      "bare name taken yields first index",
			assistant: "claude",
			tabs:      []*Tab{tab("claude", "claude")},
			want:      "claude 1",
		},
		{
			name:      "empty tab name falls back to assistant for collision",
			assistant: "claude",
			tabs:      []*Tab{tab("claude", "")},
			want:      "claude 1",
		},
		{
			name:      "skips already-used index",
			assistant: "claude",
			tabs:      []*Tab{tab("claude", "claude"), tab("claude", "claude 1")},
			want:      "claude 2",
		},
		{
			name:      "fills a gap in the index sequence",
			assistant: "claude",
			tabs:      []*Tab{tab("claude", "claude"), tab("claude", "claude 2")},
			want:      "claude 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextAssistantName(tt.assistant, tt.tabs)
			if got != tt.want {
				t.Fatalf("nextAssistantName(%q, %d tabs) = %q, want %q", tt.assistant, len(tt.tabs), got, tt.want)
			}
		})
	}
}

// TestCreateAgentTab_NilWorkspaceReturnsError verifies the guard clause: with no
// workspace selected, createAgentTab returns a command that resolves to a pure
// messages.Error without touching tmux, so it is safe to invoke here.
func TestCreateAgentTab_NilWorkspaceReturnsError(t *testing.T) {
	m := newTestModel()

	cmd := m.createAgentTab("claude", nil)
	if cmd == nil {
		t.Fatal("expected a command even when workspace is nil")
	}

	errMsg, ok := cmd().(messages.Error)
	if !ok {
		t.Fatalf("expected messages.Error for nil workspace, got %T", cmd())
	}
	if errMsg.Context != "creating agent" {
		t.Fatalf("error context = %q, want %q", errMsg.Context, "creating agent")
	}
	if errMsg.Err == nil || errMsg.Err.Error() != "no workspace selected" {
		t.Fatalf("error = %v, want \"no workspace selected\"", errMsg.Err)
	}
}

// TestCreateAgentTabWithSession_NilWorkspaceReturnsError mirrors the guard for the
// lower-level entry point regardless of the other arguments supplied.
func TestCreateAgentTabWithSession_NilWorkspaceReturnsError(t *testing.T) {
	tests := []struct {
		name        string
		assistant   string
		sessionName string
		displayName string
		activate    bool
	}{
		{name: "defaults", assistant: "claude", sessionName: "", displayName: "", activate: true},
		{name: "explicit session and name", assistant: "codex", sessionName: "sess-1", displayName: "Codex", activate: false},
		{name: "empty assistant", assistant: "", sessionName: "", displayName: "", activate: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()

			cmd := m.createAgentTabWithSession(tt.assistant, nil, tt.sessionName, tt.displayName, tt.activate)
			if cmd == nil {
				t.Fatal("expected a command even when workspace is nil")
			}

			errMsg, ok := cmd().(messages.Error)
			if !ok {
				t.Fatalf("expected messages.Error for nil workspace, got %T", cmd())
			}
			if errMsg.Context != "creating agent" {
				t.Fatalf("error context = %q, want %q", errMsg.Context, "creating agent")
			}
		})
	}
}

// TestCreateAgentTabWithSession_NonNilWorkspaceReturnsCommand confirms the
// synchronous setup (terminal metrics, tab-id and session-name derivation) runs
// without panicking for a real workspace and returns a non-nil command. The
// returned command is intentionally NOT invoked: its body calls into the agent
// manager, which execs the tmux CLI, so running it would require a live tmux
// server. Asserting the command is produced exercises every synchronous branch
// up to the closure boundary.
func TestCreateAgentTabWithSession_NonNilWorkspaceReturnsCommand(t *testing.T) {
	tests := []struct {
		name        string
		sessionName string
	}{
		{name: "derives session name", sessionName: ""},
		{name: "honors caller session name", sessionName: "preset-session"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			ws := newTestWorkspace("ws", "/repo/ws")

			cmd := m.createAgentTabWithSession("claude", ws, tt.sessionName, "Claude", true)
			if cmd == nil {
				t.Fatal("expected a non-nil command for a real workspace")
			}
			// Do not invoke cmd(): it would exec tmux via the agent manager.
		})
	}
}

// TestCreateAgentTab_NonNilWorkspaceReturnsCommand checks the convenience wrapper
// forwards through to createAgentTabWithSession and produces a command for a real
// workspace. As above, the command is not invoked because its body execs tmux.
func TestCreateAgentTab_NonNilWorkspaceReturnsCommand(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")

	if cmd := m.createAgentTab("claude", ws); cmd == nil {
		t.Fatal("expected a non-nil command for a real workspace")
	}
}
