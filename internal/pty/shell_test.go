package pty

import "testing"

func TestLoginShellCommand(t *testing.T) {
	tests := []struct {
		name    string
		shell   string
		want    string
		wantErr bool
	}{
		{
			name:  "absolute path",
			shell: "/bin/zsh",
			want:  "exec '/bin/zsh' -l",
		},
		{
			name:  "quotes metacharacters",
			shell: "/tmp/zsh; touch /tmp/pwned",
			want:  "exec '/tmp/zsh; touch /tmp/pwned' -l",
		},
		{
			name:  "quotes single quote",
			shell: "/tmp/z'sh",
			want:  "exec '/tmp/z'\\''sh' -l",
		},
		{
			name:    "rejects relative path",
			shell:   "zsh",
			wantErr: true,
		},
		{
			name:    "rejects nul",
			shell:   "/bin/zsh\x00suffix",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoginShellCommand(tt.shell)
			if tt.wantErr {
				if err == nil {
					t.Fatal("LoginShellCommand() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("LoginShellCommand() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("LoginShellCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoginShellCommandFromEnvDefault(t *testing.T) {
	t.Setenv("SHELL", "")

	got, err := LoginShellCommandFromEnv()
	if err != nil {
		t.Fatalf("LoginShellCommandFromEnv() error = %v", err)
	}
	want := "exec '" + defaultLoginShell + "' -l"
	if got != want {
		t.Fatalf("LoginShellCommandFromEnv() = %q, want %q", got, want)
	}
}
