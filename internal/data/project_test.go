package data

import (
	"testing"
)

func TestNewProject(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantName string
	}{
		{
			name:     "simple path",
			path:     "/home/user/myproject",
			wantName: "myproject",
		},
		{
			name:     "nested path",
			path:     "/home/user/code/repos/my-repo",
			wantName: "my-repo",
		},
		{
			name:     "root path",
			path:     "/",
			wantName: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProject(tt.path)
			if p.Name != tt.wantName {
				t.Errorf("NewProject().Name = %v, want %v", p.Name, tt.wantName)
			}
			if p.Path != tt.path {
				t.Errorf("NewProject().Path = %v, want %v", p.Path, tt.path)
			}
			if len(p.Workspaces) != 0 {
				t.Errorf("NewProject().Workspaces should be empty, got %d", len(p.Workspaces))
			}
		})
	}
}
