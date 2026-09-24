package tmux

import "testing"

func TestIsNoServerError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "linux no server",
			err:  wrappedErr("show-options -g @key: no server running on /tmp/tmux-1000/default"),
			want: true,
		},
		{
			name: "macos error connecting",
			err:  wrappedErr("display-message -p: error connecting to /private/tmp/tmux-501/amux (No such file or directory)"),
			want: true,
		},
		{
			name: "connection refused",
			err:  wrappedErr("set-option -g (multi): connection refused"),
			want: true,
		},
		{
			name: "other error",
			err:  wrappedErr("set-option -g @amux_activity_owner: invalid option"),
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNoServerError(tt.err); got != tt.want {
				t.Fatalf("IsNoServerError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func wrappedErr(message string) error { return testErr(message) }

type testErr string

func (e testErr) Error() string { return string(e) }

func TestStderrClassifiers(t *testing.T) {
	tests := []struct {
		name       string
		stderr     string
		session    bool
		optMissing bool
	}{
		{"session not found", "session not found: amux-x", true, false},
		{"no such session", "no such session: amux-x", true, false},
		{"cant find session", "can't find session amux-x", true, false},
		{"invalid option", "invalid option: @amux_x", false, true},
		{"unknown option", "unknown option @amux_x", false, true},
		{"mixed case session", "Session Not Found", true, false},
		{"unrelated", "lost server", false, false},
		{"empty", "", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSessionNotFoundStderr(tt.stderr); got != tt.session {
				t.Errorf("isSessionNotFoundStderr(%q) = %v, want %v", tt.stderr, got, tt.session)
			}
			if got := isOptionMissingStderr(tt.stderr); got != tt.optMissing {
				t.Errorf("isOptionMissingStderr(%q) = %v, want %v", tt.stderr, got, tt.optMissing)
			}
		})
	}
}
