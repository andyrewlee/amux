package ptyio

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

// The dead-session policy both panes share: reattach attaches only to a live
// session. This is the convergence point for the historical center/sidebar
// drift (center killed+recreated, sidebar refused) — a regression here
// silently re-splits the two paths.
func TestSessionAttachable(t *testing.T) {
	tests := []struct {
		name  string
		state tmux.SessionState
		want  bool
	}{
		{"live session", tmux.SessionState{Exists: true, HasLivePane: true}, true},
		{"missing", tmux.SessionState{Exists: false, HasLivePane: false}, false},
		{"dead pane", tmux.SessionState{Exists: true, HasLivePane: false}, false},
		{"missing but pane flag set", tmux.SessionState{Exists: false, HasLivePane: true}, false},
	}
	for _, tt := range tests {
		if got := SessionAttachable(tt.state); got != tt.want {
			t.Errorf("%s: SessionAttachable(%+v) = %v, want %v", tt.name, tt.state, got, tt.want)
		}
	}
}

func TestAttachSessionTags(t *testing.T) {
	repo := t.TempDir()
	ws := &data.Workspace{
		Name: "ws-one",
		Repo: repo,
		Root: repo,
	}
	fresh := AttachSessionTags(ws, "tab-1", "agent", "claude", "inst", true)
	if fresh.CreatedAt == 0 {
		t.Fatal("fresh attach must stamp CreatedAt")
	}
	if fresh.WorkspaceID != string(ws.ID()) || fresh.TabID != "tab-1" || fresh.Type != "agent" ||
		fresh.Assistant != "claude" || fresh.InstanceID != "inst" || fresh.SessionOwner != "inst" {
		t.Fatalf("tags = %+v", fresh)
	}
	if fresh.WorkspaceName != "ws-one" || fresh.ProjectName != filepath.Base(repo) {
		t.Fatalf("display tags = %+v, want ws-one / %s", fresh, filepath.Base(repo))
	}
	if fresh.LeaseAtMS == 0 {
		t.Fatal("lease heartbeat must always refresh")
	}

	reattach := AttachSessionTags(ws, "tab-1", "terminal", "terminal", "inst", false)
	if reattach.CreatedAt != 0 {
		t.Fatal("reattach must leave CreatedAt zero so the live session keeps its original creation time")
	}
	if reattach.LeaseAtMS == 0 {
		t.Fatal("lease heartbeat must refresh on reattach too")
	}
}

// The ownership policy both panes share on every name-match attach: the
// session must carry @amux=1 and (when set) a matching @amux_workspace. A
// foreign session squatting the amux name — predictable on a shared tmux
// server — must fail the check rather than be attached to.
func TestSessionOwned(t *testing.T) {
	old := SessionsWithTagsFn
	t.Cleanup(func() { SessionsWithTagsFn = old })

	tests := []struct {
		name  string
		rows  []tmux.SessionTagValues
		query error
		wsIDs []string
		want  bool
	}{
		{
			name: "owned matching wsID",
			rows: []tmux.SessionTagValues{
				{Name: "amux-ws1-t1", Tags: map[string]string{"@amux_workspace": "ws1"}},
			},
			wsIDs: []string{"ws1"},
			want:  true,
		},
		{
			name: "owned via second ID form",
			rows: []tmux.SessionTagValues{
				{Name: "amux-ws1-t1", Tags: map[string]string{"@amux_workspace": "ws1-legacy"}},
			},
			wsIDs: []string{"ws1", "ws1-legacy"},
			want:  true,
		},
		{
			name: "empty workspace tag is legacy-accepted",
			rows: []tmux.SessionTagValues{
				{Name: "amux-ws1-t1", Tags: map[string]string{"@amux_workspace": ""}},
			},
			wsIDs: []string{"ws1"},
			want:  true,
		},
		{
			name: "foreign wsID refused",
			rows: []tmux.SessionTagValues{
				{Name: "amux-ws1-t1", Tags: map[string]string{"@amux_workspace": "ws-other"}},
			},
			wsIDs: []string{"ws1"},
			want:  false,
		},
		{
			name:  "untagged session not in rows refused",
			rows:  []tmux.SessionTagValues{},
			wsIDs: []string{"ws1"},
			want:  false,
		},
		{
			name: "other sessions do not match by name",
			rows: []tmux.SessionTagValues{
				{Name: "amux-ws1-t2", Tags: map[string]string{"@amux_workspace": "ws1"}},
			},
			wsIDs: []string{"ws1"},
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SessionsWithTagsFn = func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
				if match["@amux"] != "1" {
					t.Fatalf("ownership query must filter on @amux=1, got %v", match)
				}
				return tt.rows, tt.query
			}
			got, err := SessionOwned("amux-ws1-t1", tt.wsIDs, tmux.Options{})
			if err != nil {
				t.Fatalf("SessionOwned error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("SessionOwned = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("query error propagates", func(t *testing.T) {
		SessionsWithTagsFn = func(map[string]string, []string, tmux.Options) ([]tmux.SessionTagValues, error) {
			return nil, errors.New("tmux query failed")
		}
		if _, err := SessionOwned("amux-ws1-t1", []string{"ws1"}, tmux.Options{}); err == nil {
			t.Fatal("expected the tag-query error to propagate (fail closed)")
		}
	})
}
