package workspacesvc

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/testutil"
)

// TestPrependPrimaryCheckout_AdoptsConcurrentRecord is the lost-update
// regression for the primary-checkout save: when a record appears between
// the lookup and the write, the create-if-absent transaction reports it and
// the service merges the winner's stored fields instead of overwriting them
// with the sparse transient snapshot.
func TestPrependPrimaryCheckout_AdoptsConcurrentRecord(t *testing.T) {
	repo := testutil.InitRepo(t)

	concurrent := &data.Workspace{
		Repo:     repo,
		Root:     repo,
		OpenTabs: []data.TabInfo{{Assistant: "claude", Name: "work"}},
		Env:      map[string]string{"TOKEN": "kept"},
	}
	var loadCalls int
	store := &testutil.FakeWorkspaceStore{
		SaveIfAbsentFunc: func(ws *data.Workspace) (*data.Workspace, bool, error) {
			return concurrent, false, nil
		},
		LoadMetadataForFunc: func(ws *data.Workspace) (bool, error) {
			loadCalls++
			ws.OpenTabs = concurrent.OpenTabs
			ws.Env = concurrent.Env
			return true, nil
		},
	}
	svc := New(nil, store, nil, t.TempDir())

	out := svc.prependPrimaryCheckout(repo, nil)
	if len(out) != 1 {
		t.Fatalf("workspaces = %d, want the transient primary only", len(out))
	}
	primary := out[0]
	if loadCalls != 1 {
		t.Fatalf("LoadMetadataFor calls = %d, want 1 (merge of the found record)", loadCalls)
	}
	if len(primary.OpenTabs) != 1 || primary.Env["TOKEN"] != "kept" {
		t.Fatalf("primary checkout lost the concurrent record's fields: %+v", primary)
	}
}

// TestPrependPrimaryCheckout_CreatesWhenAbsent covers the other half: no
// competing record → the transient snapshot is persisted as the new record
// and no merge pass runs.
func TestPrependPrimaryCheckout_CreatesWhenAbsent(t *testing.T) {
	repo := testutil.InitRepo(t)

	var loadCalls, saveCalls int
	store := &testutil.FakeWorkspaceStore{
		SaveIfAbsentFunc: func(ws *data.Workspace) (*data.Workspace, bool, error) {
			saveCalls++
			return ws, true, nil
		},
		LoadMetadataForFunc: func(ws *data.Workspace) (bool, error) {
			loadCalls++
			return false, nil
		},
	}
	svc := New(nil, store, nil, t.TempDir())

	out := svc.prependPrimaryCheckout(repo, nil)
	if len(out) != 1 {
		t.Fatalf("workspaces = %d, want the transient primary only", len(out))
	}
	if saveCalls != 1 || loadCalls != 0 {
		t.Fatalf("SaveIfAbsent=%d LoadMetadataFor=%d, want a single create and no merge", saveCalls, loadCalls)
	}
}
