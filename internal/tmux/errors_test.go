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
