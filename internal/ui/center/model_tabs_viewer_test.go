package center

import (
	"testing"

	"github.com/andyrewlee/amux/internal/config"
)

func TestViewerLaunch(t *testing.T) {
	tests := []struct {
		name      string
		viewer    string // value passed to SetViewerCommand; "" = leave default
		path      string
		wantCmd   string
		wantLabel string
	}{
		{name: "default vim", path: "/tmp/f.txt", wantCmd: "vim -- '/tmp/f.txt'", wantLabel: "vim"},
		{name: "configured nvim", viewer: "nvim", path: "/a b/f.txt", wantCmd: "nvim -- '/a b/f.txt'", wantLabel: "nvim"},
		{name: "fragment with flags", viewer: "less -R", path: "/f", wantCmd: "less -R -- '/f'", wantLabel: "less"},
		{name: "empty falls back to vim", viewer: "   ", path: "/f", wantCmd: "vim -- '/f'", wantLabel: "vim"},
		{name: "single quote in path escaped", viewer: "vim", path: "/it's/f", wantCmd: `vim -- '/it'\''s/f'`, wantLabel: "vim"},
		{name: "command fragment not quoted", viewer: "nvim --clean", path: "/f", wantCmd: "nvim --clean -- '/f'", wantLabel: "nvim"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(&config.Config{})
			if tt.viewer != "" {
				m.SetViewerCommand(tt.viewer)
			}
			cmd, label := m.viewerLaunch(tt.path)
			if cmd != tt.wantCmd {
				t.Errorf("cmd = %q, want %q", cmd, tt.wantCmd)
			}
			if label != tt.wantLabel {
				t.Errorf("label = %q, want %q", label, tt.wantLabel)
			}
		})
	}
}
