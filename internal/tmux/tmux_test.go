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

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "''"},
		{"hello", "'hello'"},
		{"hello world", "'hello world'"},
		{"it's", "'it'\\''s'"},
		{"path/to/file", "'path/to/file'"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := shellQuote(tt.input)
			if result != tt.expected {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.input, result, tt.expected)
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

func TestClientCommandWithOptions(t *testing.T) {
	opts := Options{
		ServerName:      "test-server",
		ConfigPath:      "/dev/null",
		HideStatus:      true,
		DisableMouse:    true,
		DefaultTerminal: "xterm-256color",
	}

	cmd := ClientCommandWithOptions("test-session", "/tmp/work", "echo hello", opts)

	// Should use atomic new-session -Ad
	if !strings.Contains(cmd, "new-session -Ads") {
		t.Error("Command should use atomic new-session -Ads")
	}

	// Should disable prefix per-session (not globally)
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
	// Should use new-session -Ad when detaching
	if !strings.Contains(cmd, "new-session -Ads") {
		t.Error("Command should use new-session -Ads when detaching")
	}

	// Should use && not ; for chaining
	if !strings.Contains(cmd, " && ") {
		t.Error("Command should chain with && not ;")
	}

	// Should include server name
	if !strings.Contains(cmd, "-L 'test-server'") {
		t.Error("Command should include server name")
	}
}

func TestClientCommandWithTags(t *testing.T) {
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

	cmd := ClientCommandWithTags("test-session", "/tmp/work", "echo hello", opts, tags)

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

func TestClientCommandWithTagsAttachShared(t *testing.T) {
	opts := Options{
		ServerName:      "test-server",
		ConfigPath:      "/dev/null",
		HideStatus:      true,
		DisableMouse:    true,
		DefaultTerminal: "xterm-256color",
	}
	cmd := ClientCommandWithTagsAttach("test-session", "/tmp/work", "echo hello", opts, SessionTags{}, false)
	if strings.Contains(cmd, "attach -dt") {
		t.Error("Command should not detach other clients when detachExisting=false")
	}
	if !strings.Contains(cmd, "attach -t") {
		t.Error("Command should attach without detaching other clients")
	}
	if strings.Contains(cmd, "new-session -Ads") {
		t.Error("Command should not detach on new-session when detachExisting=false")
	}
	if !strings.Contains(cmd, "new-session -As") {
		t.Error("Command should use new-session -As when detachExisting=false")
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
	_, err := CapturePane("no-such-session-ever", opts)
	// Should return an error (session doesn't exist)
	if err == nil {
		t.Error("CapturePane with nonexistent session should return an error")
	}
}
