package workspacesvc

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
		{name: "slash bypass cleaned dot", project: &data.Project{Name: "repo/.", Path: ""}, want: "", wantOK: false},
		{name: "slash bypass cleaned parent", project: &data.Project{Name: "foo/../bar", Path: ""}, want: "", wantOK: false},
		{name: "backslash", project: &data.Project{Name: "bad\\name", Path: ""}, want: "", wantOK: false},
		{name: "empty name root path", project: &data.Project{Name: "", Path: "/"}, want: "", wantOK: false},
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
			got := pathWithinStrict(tt.root, tt.candidate)
			if got != tt.want {
				t.Fatalf("pathWithinStrict(%q, %q) = %v, want %v", tt.root, tt.candidate, got, tt.want)
			}
		})
	}
}

func TestIsManagedWorkspaceChildPathForProject(t *testing.T) {
	tests := []struct {
		name           string
		workspacesRoot string
		project        *data.Project
		path           string
		want           bool
	}{
		{
			name:           "child workspace path",
			workspacesRoot: "/tmp/workspaces",
			project:        &data.Project{Name: "repo", Path: "/tmp/repo"},
			path:           "/tmp/workspaces/repo/feature",
			want:           true,
		},
		{
			name:           "project managed root itself",
			workspacesRoot: "/tmp/workspaces",
			project:        &data.Project{Name: "repo", Path: "/tmp/repo"},
			path:           "/tmp/workspaces/repo",
			want:           false,
		},
		{
			name:           "deep descendant inside live workspace",
			workspacesRoot: "/tmp/workspaces",
			project:        &data.Project{Name: "repo", Path: "/tmp/repo"},
			path:           "/tmp/workspaces/repo/feature/src",
			want:           false,
		},
		{
			name:           "deep descendant via project path basename alias",
			workspacesRoot: "/tmp/workspaces",
			project:        &data.Project{Name: "repo-link", Path: "/tmp/repo-real"},
			path:           "/tmp/workspaces/repo-real/feature/src",
			want:           false,
		},
		{
			name:           "child via project path basename alias",
			workspacesRoot: "/tmp/workspaces",
			project:        &data.Project{Name: "repo-link", Path: "/tmp/repo-real"},
			path:           "/tmp/workspaces/repo-real/feature",
			want:           true,
		},
		{
			name:           "legacy root disabled for destructive cleanup",
			workspacesRoot: "",
			project:        &data.Project{Name: "repo", Path: "/tmp/repo"},
			path:           "/tmp/workspaces/repo/feature",
			want:           false,
		},
		{
			name:           "path outside root",
			workspacesRoot: "/tmp/workspaces",
			project:        &data.Project{Name: "repo", Path: "/tmp/repo"},
			path:           "/tmp/other/repo/feature",
			want:           false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isManagedWorkspaceChildPathForProject(tt.workspacesRoot, tt.project, tt.path)
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestShouldSurfaceWorkspace_ForgedWorktreePaths proves a forged
// `git worktree list` entry — the delivery vector is a bundled .git shipping
// its own .git/worktrees admin dirs — can never become a surfaced row: the
// project root itself and any subpath of a live workspace are rejected,
// while a legitimate direct-child worktree still surfaces.
func TestShouldSurfaceWorkspace_ForgedWorktreePaths(t *testing.T) {
	svc := New(nil, nil, nil, "/tmp/workspaces")
	projectPath := "/tmp/repo"

	cases := []struct {
		name string
		ws   *data.Workspace
		want bool
	}{
		{
			name: "legitimate direct child",
			ws:   &data.Workspace{Repo: projectPath, Root: "/tmp/workspaces/repo/feature"},
			want: true,
		},
		{
			// Flat depth-1 worktrees under the managed root are legitimate:
			// git-discovered worktrees can live outside any project segment.
			name: "flat child of managed root",
			ws:   &data.Workspace{Repo: projectPath, Root: "/tmp/workspaces/feature"},
			want: true,
		},
		{
			name: "forged project root entry",
			ws:   &data.Workspace{Repo: projectPath, Root: "/tmp/workspaces/repo"},
			want: false,
		},
		{
			name: "forged subdirectory of a live workspace",
			ws:   &data.Workspace{Repo: projectPath, Root: "/tmp/workspaces/repo/feature/src"},
			want: false,
		},
		{
			// Depth-2 under a different segment still surfaces (the record's
			// Repo matched this project) — but the stricter per-project gate
			// on destructive paths means it cannot be deleted through it.
			name: "cross-segment grandchild surfaces",
			ws:   &data.Workspace{Repo: projectPath, Root: "/tmp/workspaces/repoX/feature"},
			want: true,
		},
		{
			name: "forged path outside managed root",
			ws:   &data.Workspace{Repo: projectPath, Root: "/home/user/important"},
			want: false,
		},
		{
			name: "empty root",
			ws:   &data.Workspace{Repo: projectPath, Root: ""},
			want: false,
		},
		{
			name: "nil workspace",
			ws:   nil,
			want: false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := svc.shouldSurfaceWorkspace(projectPath, tt.ws); got != tt.want {
				t.Fatalf("shouldSurfaceWorkspace() = %v, want %v", got, tt.want)
			}
		})
	}

	// The primary checkout is exempt — it never lives under the managed root.
	primary := &data.Workspace{Repo: projectPath, Root: projectPath}
	if !svc.shouldSurfaceWorkspace(projectPath, primary) {
		t.Fatal("primary checkout should always surface")
	}
}
