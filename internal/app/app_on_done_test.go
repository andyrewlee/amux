package app

import (
	"errors"
	"testing"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"

	tea "charm.land/bubbletea/v2"
)

// drainOnDoneCmd executes cmd and flattens one level of tea.BatchMsg so tests
// can inspect the messages a handler's command emits.
func drainOnDoneCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		if m := sub(); m != nil {
			out = append(out, m)
		}
	}
	return out
}

type recordedOnDoneFire struct {
	wsRoot      string
	sessionName string
}

// stubOnDoneHook swaps the runOnDoneHook seam for a recorder, mirroring the
// fakeSetAgentStateTag pattern in app_tmux_activity_agent_state_tag_test.go.
func stubOnDoneHook(t *testing.T, err error) *[]recordedOnDoneFire {
	t.Helper()
	orig := runOnDoneHook
	var recorded []recordedOnDoneFire
	runOnDoneHook = func(ws *data.Workspace, sessionName string, _ *workspacesvc.Service) error {
		recorded = append(recorded, recordedOnDoneFire{ws.Root, sessionName})
		return err
	}
	t.Cleanup(func() { runOnDoneHook = orig })
	return &recorded
}

func onDoneTestApp(ws *data.Workspace) *App {
	return &App{
		projects: []data.Project{{Workspaces: []data.Workspace{*ws}}},
	}
}

func TestOnDoneHookCmd_FiresOnWorkingToDone(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	fires := stubOnDoneHook(t, nil)
	app := onDoneTestApp(ws)

	session := "amux-" + string(ws.ID()) + "-tab-1"
	cmd := app.onDoneHookCmd([]agentStateTagChange{
		{sessionName: session, state: activity.StateDone, prev: activity.StateWorking},
	})
	if cmd == nil {
		t.Fatal("expected a hook cmd for a working→done edge")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("successful fire should return nil msg, got %T", msg)
	}
	if len(*fires) != 1 {
		t.Fatalf("hook fired %d times, want exactly once", len(*fires))
	}
	got := (*fires)[0]
	if got.sessionName != session || got.wsRoot != ws.Root {
		t.Fatalf("hook fired with %+v, want session %q root %q", got, session, ws.Root)
	}
}

func TestOnDoneHookCmd_NonWorkingEdgesDoNotFire(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	app := onDoneTestApp(ws)
	session := "amux-" + string(ws.ID()) + "-tab-1"

	for _, tc := range []struct {
		name        string
		state, prev activity.AgentState
	}{
		// The restart case: an agent that finished before amux started scans
		// in as idle→done. Firing a hook there could re-run side-effecting
		// commands for completions nobody watched — deliberately excluded.
		{"idle→done (first scan)", activity.StateDone, activity.StateIdle},
		{"done→idle", activity.StateIdle, activity.StateDone},
		{"working→idle", activity.StateIdle, activity.StateWorking},
		{"idle→working", activity.StateWorking, activity.StateIdle},
		{"working→working", activity.StateWorking, activity.StateWorking},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if cmd := app.onDoneHookCmd([]agentStateTagChange{
				{sessionName: session, state: tc.state, prev: tc.prev},
			}); cmd != nil {
				t.Fatal("expected no hook cmd")
			}
		})
	}
}

func TestOnDoneHookCmd_UnknownWorkspaceSkipped(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	app := onDoneTestApp(ws)
	// A session name whose wsID segment matches no project workspace.
	cmd := app.onDoneHookCmd([]agentStateTagChange{
		{sessionName: "amux-0000000000000000-tab-1", state: activity.StateDone, prev: activity.StateWorking},
	})
	if cmd != nil {
		t.Fatal("expected nil cmd when the session's workspace is unknown")
	}
}

func TestOnDoneHookCmd_ErrorReportsOnce(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	stubOnDoneHook(t, errors.New("spawn failed"))
	app := onDoneTestApp(ws)
	session := "amux-" + string(ws.ID()) + "-tab-1"

	cmd := app.onDoneHookCmd([]agentStateTagChange{
		{sessionName: session, state: activity.StateDone, prev: activity.StateWorking},
	})
	msg := cmd()
	res, ok := msg.(messages.WorkspaceOnDoneResult)
	if !ok {
		t.Fatalf("expected WorkspaceOnDoneResult, got %T", msg)
	}
	if res.SessionName != session || res.Err == nil {
		t.Fatalf("result = %+v, want session %q with error", res, session)
	}
}

func TestHandleWorkspaceOnDoneResult_UntrustedOffersTrustDialog(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws"}
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	errUntrusted := &process.ScriptsNotTrustedError{Repo: ws.Repo, Command: "notify-send done", ConfigHash: "abc123"}

	cmd := h.app.handleWorkspaceOnDoneResult(messages.WorkspaceOnDoneResult{
		Workspace: ws, SessionName: "s1", Err: errUntrusted,
	})
	if cmd == nil {
		t.Fatal("expected cmds for the trust skip")
	}
	// First cmd is the warning toast; the batch also carries the trust-dialog
	// message so the user can approve the repo for the next edge.
	found := false
	for _, m := range drainOnDoneCmd(cmd) {
		if show, ok := m.(messages.ShowTrustScriptsDialog); ok {
			found = true
			if show.Workspace != ws || show.ConfigHash != "abc123" {
				t.Fatalf("trust dialog msg = %+v", show)
			}
		}
	}
	if !found {
		t.Fatal("expected a ShowTrustScriptsDialog message")
	}
}
