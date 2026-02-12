package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestProjectNameSegment(t *testing.T) {
	tests := []struct {
		name    string
		project *data.Project
		want    string
		wantOK  bool
	}{
		{name: "nil project", project: nil, want: "", wantOK: false},
		{name: "normal", project: &data.Project{Name: "myrepo", Path: "/tmp/myrepo"}, want: "myrepo", wantOK: true},
		{name: "empty name fallback to path", project: &data.Project{Name: "", Path: "/tmp/fallback"}, want: "fallback", wantOK: true},
		{name: "dot", project: &data.Project{Name: ".", Path: ""}, want: "", wantOK: false},
		{name: "dotdot", project: &data.Project{Name: "..", Path: ""}, want: "", wantOK: false},
		{name: "slash", project: &data.Project{Name: "bad/name", Path: ""}, want: "", wantOK: false},
		{name: "backslash", project: &data.Project{Name: "bad\\name", Path: ""}, want: "", wantOK: false},
		{name: "empty name empty path", project: &data.Project{Name: "", Path: ""}, want: "", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := projectNameSegment(tt.project)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("segment = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsPathWithin(t *testing.T) {
	tests := []struct {
		name      string
		root      string
		candidate string
		want      bool
	}{
		{name: "nested", root: "/a/b", candidate: "/a/b/c", want: true},
		{name: "same path", root: "/a/b", candidate: "/a/b", want: false},
		{name: "sibling", root: "/a/b", candidate: "/a/c", want: false},
		{name: "parent", root: "/a/b/c", candidate: "/a/b", want: false},
		{name: "empty root", root: "", candidate: "/a/b", want: false},
		{name: "empty candidate", root: "/a/b", candidate: "", want: false},
		{name: "deeply nested", root: "/a/b", candidate: "/a/b/c/d/e", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPathWithin(tt.root, tt.candidate)
			if got != tt.want {
				t.Fatalf("isPathWithin(%q, %q) = %v, want %v", tt.root, tt.candidate, got, tt.want)
			}
		})
	}
}

func TestIsManagedWorkspacePathForProject(t *testing.T) {
	tests := []struct {
		name           string
		workspacesRoot string
		project        *data.Project
		path           string
		want           bool
	}{
		{
			name:           "within root",
			workspacesRoot: "/tmp/workspaces",
			project:        &data.Project{Name: "repo", Path: "/tmp/repo"},
			path:           "/tmp/workspaces/repo/feature",
			want:           true,
		},
		{
			name:           "outside root",
			workspacesRoot: "/tmp/workspaces",
			project:        &data.Project{Name: "repo", Path: "/tmp/repo"},
			path:           "/tmp/other/repo/feature",
			want:           false,
		},
		{
			name:           "empty workspacesRoot legacy",
			workspacesRoot: "",
			project:        &data.Project{Name: "repo", Path: "/tmp/repo"},
			path:           "/anywhere/feature",
			want:           true,
		},
		{
			name:           "nil project",
			workspacesRoot: "/tmp/workspaces",
			project:        nil,
			path:           "/tmp/workspaces/repo/feature",
			want:           false,
		},
		{
			name:           "empty path",
			workspacesRoot: "/tmp/workspaces",
			project:        &data.Project{Name: "repo", Path: "/tmp/repo"},
			path:           "",
			want:           false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isManagedWorkspacePathForProject(tt.workspacesRoot, tt.project, tt.path)
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
