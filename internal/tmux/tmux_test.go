package tmux

import (
	"strings"
	"testing"
)

func TestSessionName(t *testing.T) {
	tests := []struct {
		name     string
		parts    []string
		expected string
	}{
		{
			name:     "empty parts",
			parts:    []string{},
			expected: "amux",
		},
		{
			name:     "single part",
			parts:    []string{"amux"},
			expected: "amux",
		},
		{
			name:     "multiple parts",
			parts:    []string{"amux", "ws-123", "tab-456"},
			expected: "amux-ws-123-tab-456",
		},
		{
			name:     "parts with spaces are trimmed",
			parts:    []string{"  amux  ", "  ws  "},
			expected: "amux-ws",
		},
		{
			name:     "empty parts are skipped",
			parts:    []string{"amux", "", "ws"},
			expected: "amux-ws",
		},
		{
			name:     "special characters are sanitized",
			parts:    []string{"amux", "my/workspace", "tab:1"},
			expected: "amux-my-workspace-tab-1",
		},
		{
			name:     "uppercase is lowercased",
			parts:    []string{"AMUX", "WS"},
			expected: "amux-ws",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SessionName(tt.parts...)
			if result != tt.expected {
				t.Errorf("SessionName(%v) = %q, want %q", tt.parts, result, tt.expected)
			}
		})
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"HELLO", "hello"},
		{"hello-world", "hello-world"},
		{"hello_world", "hello_world"},
		{"hello/world", "hello-world"},
		{"hello:world", "hello-world"},
		{"hello world", "hello-world"},
		{"---hello---", "hello"},
		{"123", "123"},
		{"a1b2c3", "a1b2c3"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitize(tt.input)
			if result != tt.expected {
				t.Errorf("sanitize(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	if opts.ServerName == "" {
		t.Error("ServerName should not be empty")
	}
	if opts.ConfigPath == "" {
		t.Error("ConfigPath should not be empty")
	}
	if opts.DefaultTerminal != "xterm-256color" {
		t.Errorf("DefaultTerminal = %q, want %q", opts.DefaultTerminal, "xterm-256color")
	}
	if !opts.HideStatus {
		t.Error("HideStatus should be true")
	}
	if !opts.DisableMouse {
		t.Error("DisableMouse should be true")
	}
}

func TestNewClientCommand(t *testing.T) {
	opts := Options{
		ServerName:      "test-server",
		ConfigPath:      "/dev/null",
		HideStatus:      true,
		DisableMouse:    true,
		DefaultTerminal: "xterm-256color",
	}

	cmd := NewClientCommand("test-session", ClientCommandParams{
		WorkDir:        "/tmp/work",
		Command:        "echo hello",
		Options:        opts,
		DetachExisting: true,
	})

	// Should create detached before the final attach so server options can be
	// set before tmux computes client features.
	if !strings.Contains(cmd, "has-session -t '=test-session'") {
		t.Error("Command should check for an existing session")
	}
	if strings.Count(cmd, "has-session -t '=test-session'") < 2 {
		t.Error("Command should retry has-session after detached create races")
	}
	if !strings.Contains(cmd, "new-session -ds") {
		t.Error("Command should create the session detached before attaching")
	}

	// Should disable prefix per-session (not globally) with exact-match target
	if !strings.Contains(cmd, "set-option -t 'test-session' prefix None") {
		t.Error("Command should disable prefix for session")
	}
	if !strings.Contains(cmd, "set-option -t 'test-session' prefix2 None") {
		t.Error("Command should disable prefix2 for session")
	}

	// Should use attach -d (detach other clients)
	if !strings.Contains(cmd, "attach -dt") {
		t.Error("Command should use attach -dt to detach other clients")
	}
	// Should use && not ; for chaining
	if !strings.Contains(cmd, " && ") {
		t.Error("Command should chain with && not ;")
	}

	// Should include server name
	if !strings.Contains(cmd, "-L 'test-server'") {
		t.Error("Command should include server name")
	}
	// Should run pane command via sh -lc
	if !strings.Contains(cmd, "sh -lc 'unset TMUX TMUX_PANE; echo hello'") {
		t.Error("Command should run pane command via sh -lc with tmux env sanitized")
	}

	// Should advertise DEC 2026 sync support before attaching so tmux wraps
	// redraws in sync markers (prevents partial-frame flicker in the vterm).
	syncSet := "set-option -s 'terminal-features[16]' 'xterm*:sync'"
	syncIdx := strings.Index(cmd, syncSet)
	createIdx := strings.Index(cmd, "new-session -ds")
	attachIdx := strings.Index(cmd, "attach -dt")
	if syncIdx < 0 {
		t.Error("Command should set the sync terminal-feature")
	}
	if !strings.Contains(cmd, "|| true) &&") {
		t.Error("sync terminal-feature fallback should be grouped before continuing")
	}
	if createIdx >= 0 && syncIdx < createIdx {
		t.Error("sync terminal-feature should be set after detached session create keeps the server alive")
	}
	if attachIdx >= 0 && syncIdx > attachIdx {
		t.Error("sync terminal-feature must be set before attach (features are computed at attach time)")
	}
}

func TestNewClientCommandWithTags(t *testing.T) {
	opts := Options{
		ServerName:      "test-server",
		ConfigPath:      "/dev/null",
		HideStatus:      true,
		DisableMouse:    true,
		DefaultTerminal: "xterm-256color",
	}
	tags := SessionTags{
		WorkspaceID: "ws-1",
		TabID:       "tab-2",
		Type:        "agent",
		Assistant:   "claude",
		CreatedAt:   123,
		InstanceID:  "inst-9",
	}

	cmd := NewClientCommand("test-session", ClientCommandParams{
		WorkDir:        "/tmp/work",
		Command:        "echo hello",
		Options:        opts,
		Tags:           tags,
		DetachExisting: true,
	})

	if !strings.Contains(cmd, "@amux 1") {
		t.Error("Command should set @amux tag")
	}
	if !strings.Contains(cmd, "@amux_workspace 'ws-1'") {
		t.Error("Command should set @amux_workspace tag")
	}
	if !strings.Contains(cmd, "@amux_tab 'tab-2'") {
		t.Error("Command should set @amux_tab tag")
	}
	if !strings.Contains(cmd, "@amux_type 'agent'") {
		t.Error("Command should set @amux_type tag")
	}
	if !strings.Contains(cmd, "@amux_assistant 'claude'") {
		t.Error("Command should set @amux_assistant tag")
	}
	if !strings.Contains(cmd, "@amux_created_at '123'") {
		t.Error("Command should set @amux_created_at tag")
	}
	if !strings.Contains(cmd, "@amux_instance 'inst-9'") {
		t.Error("Command should set @amux_instance tag")
	}
}

func TestNewClientCommandWithInstanceIDOnly(t *testing.T) {
	opts := Options{
		ServerName:      "test-server",
		ConfigPath:      "/dev/null",
		HideStatus:      true,
		DisableMouse:    true,
		DefaultTerminal: "xterm-256color",
	}

	cmd := NewClientCommand("test-session", ClientCommandParams{
		WorkDir:        "/tmp/work",
		Command:        "echo hello",
		Options:        opts,
		Tags:           SessionTags{InstanceID: "inst-only"},
		DetachExisting: true,
	})

	if !strings.Contains(cmd, "@amux 1") {
		t.Error("Command should set @amux tag when only InstanceID is provided")
	}
	if !strings.Contains(cmd, "@amux_instance 'inst-only'") {
		t.Error("Command should set @amux_instance tag")
	}
}

func TestNewClientCommandSharedAttach(t *testing.T) {
	opts := Options{
		ServerName:      "test-server",
		ConfigPath:      "/dev/null",
		HideStatus:      true,
		DisableMouse:    true,
		DefaultTerminal: "xterm-256color",
	}
	cmd := NewClientCommand("test-session", ClientCommandParams{
		WorkDir:        "/tmp/work",
		Command:        "echo hello",
		Options:        opts,
		DetachExisting: false,
	})
	if strings.Contains(cmd, "attach -dt") {
		t.Error("Command should not detach other clients when detachExisting=false")
	}
	if !strings.Contains(cmd, "attach -t") {
		t.Error("Command should attach without detaching other clients")
	}
	if !strings.Contains(cmd, "new-session -ds") {
		t.Error("Command should create detached before shared attach")
	}
	syncSet := "set-option -s 'terminal-features[16]' 'xterm*:sync'"
	syncIdx := strings.Index(cmd, syncSet)
	createIdx := strings.Index(cmd, "new-session -ds")
	attachIdx := strings.Index(cmd, "attach -t")
	if syncIdx < 0 {
		t.Error("Command should set the sync terminal-feature")
	}
	if createIdx >= 0 && syncIdx < createIdx {
		t.Error("sync terminal-feature should be set after detached create keeps the server alive")
	}
	if attachIdx >= 0 && syncIdx > attachIdx {
		t.Error("sync terminal-feature must be set before shared attach")
	}
}

func TestTmuxBase(t *testing.T) {
	tests := []struct {
		name     string
		opts     Options
		contains []string
	}{
		{
			name: "with server name",
			opts: Options{ServerName: "myserver"},
			contains: []string{
				"tmux",
				"-L 'myserver'",
			},
		},
		{
			name: "with config path",
			opts: Options{ConfigPath: "/path/to/config"},
			contains: []string{
				"tmux",
				"-f '/path/to/config'",
			},
		},
		{
			name: "with both",
			opts: Options{ServerName: "myserver", ConfigPath: "/path/to/config"},
			contains: []string{
				"tmux",
				"-L 'myserver'",
				"-f '/path/to/config'",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tmuxBase(tt.opts)
			for _, want := range tt.contains {
				if !strings.Contains(result, want) {
					t.Errorf("tmuxBase() = %q, want to contain %q", result, want)
				}
			}
		})
	}
}

func TestTmuxArgs(t *testing.T) {
	opts := Options{
		ServerName: "myserver",
		ConfigPath: "/dev/null",
	}

	args := tmuxArgs(opts, "list-sessions", "-F", "#{session_name}")

	expected := []string{"-L", "myserver", "-f", "/dev/null", "list-sessions", "-F", "#{session_name}"}
	if len(args) != len(expected) {
		t.Errorf("tmuxArgs() length = %d, want %d", len(args), len(expected))
		return
	}

	for i, arg := range args {
		if arg != expected[i] {
			t.Errorf("tmuxArgs()[%d] = %q, want %q", i, arg, expected[i])
		}
	}
}

func TestInstallHint(t *testing.T) {
	hint := InstallHint()
	if hint == "" {
		t.Error("InstallHint should not be empty")
	}
}

func TestCapturePaneEmptySession(t *testing.T) {
	data, err := CapturePane("", DefaultOptions())
	if err != nil {
		t.Errorf("CapturePane with empty session should not error, got %v", err)
	}
	if data != nil {
		t.Errorf("CapturePane with empty session should return nil, got %v", data)
	}
}

func TestCapturePaneNonexistentSession(t *testing.T) {
	opts := Options{
		ServerName:     "amux-test-nonexistent",
		ConfigPath:     "/dev/null",
		CommandTimeout: 5_000_000_000, // 5s
	}
	data, err := CapturePane("no-such-session-ever", opts)
	// Should return nil (session doesn't exist, resolved via hasSession pre-check)
	if err != nil {
		t.Errorf("CapturePane with nonexistent session should not error, got %v", err)
	}
	if data != nil {
		t.Errorf("CapturePane with nonexistent session should return nil, got %v", data)
	}
}

func TestTargetHelpers(t *testing.T) {
	name := "my-session"
	if got := sessionTarget(name); got != "=my-session" {
		t.Errorf("sessionTarget(%q) = %q, want %q", name, got, "=my-session")
	}
	if got := exactSessionOptionTarget(name); got != "my-session" {
		t.Errorf("exactSessionOptionTarget(%q) = %q, want %q", name, got, "my-session")
	}
}
